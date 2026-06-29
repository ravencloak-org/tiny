package pipe

// Control-flow templating for pipe SQL: the {% if %} / {% elif %} / {% else %} /
// {% end %} blocks Tinybird pipes use (ADR 0003, Phase 2). This runs BEFORE value
// param substitution so it decides which SQL text — and which {{Type(name)}}
// placeholders — survive. A non-taken branch is dropped wholesale, so its value
// placeholders never reach the param binder and never count as "required".
//
// The block grammar (tokenizer + recursive parser) is hand-written and thin; the
// boolean inside each {% %} is delegated to github.com/expr-lang/expr, a mature
// sandboxed evaluator. We never hand-roll an expression parser (the hard, bug-prone
// part) and never string-interpolate values — control flow only shapes structure.

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/tinyraven/tinyraven/internal/model"
)

// resolveControlFlow evaluates the {% %} blocks in sql against the request params
// and returns the SQL with only the taken branches' text retained. declared is the
// pipe's full param set (used to seed the condition environment so a comparison
// against a param that wasn't supplied evaluates to false instead of erroring).
//
// Pipeline order matters (ADR 0003): callers MUST run this before {{Type(name)}}
// substitution. Placeholders inside a dropped branch disappear here, so they are
// neither bound nor required afterwards.
func resolveControlFlow(sql string, params url.Values, declared []model.Param) (string, error) {
	if !strings.Contains(sql, "{%") {
		return sql, nil // fast path: no control flow to resolve
	}
	toks, err := tokenizeTemplate(sql)
	if err != nil {
		return "", err
	}
	nodes, err := parseTemplate(toks)
	if err != nil {
		return "", err
	}
	return renderTemplate(nodes, buildCondEnv(params, declared))
}

// paramNamesInSQL returns the set of {{Type(name)}} param names present in sql,
// using the shared placeholderRe. After control flow is resolved this is the set of
// params that actually survived, i.e. the params the binder must require/bind.
func paramNamesInSQL(sql string) map[string]bool {
	out := map[string]bool{}
	for _, m := range placeholderRe.FindAllStringSubmatch(sql, -1) {
		out[m[2]] = true
	}
	return out
}

// buildCondEnv builds the expr-lang environment for condition evaluation. Every
// declared param is seeded to "" so an unsupplied param compares as empty rather
// than tripping an undefined-variable error; supplied request values overlay those.
// `defined(x)` reports whether the request supplied x as a non-empty value — per
// spec it receives x's seeded value, so it reduces to a non-empty string check.
func buildCondEnv(params url.Values, declared []model.Param) map[string]any {
	env := make(map[string]any, len(declared)+len(params)+1)
	for _, p := range declared {
		env[p.Name] = ""
	}
	for k := range params {
		env[k] = params.Get(k)
	}
	env["defined"] = func(s string) bool { return s != "" }
	return env
}

// evalCondition evaluates one {% if/elif %} boolean expression via expr-lang. Any
// compile/eval failure (bad syntax, unknown function) or a non-boolean result is
// surfaced as a clear error rather than a panic.
func evalCondition(condExpr string, env map[string]any) (bool, error) {
	program, err := expr.Compile(condExpr, expr.Env(env), expr.AllowUndefinedVariables())
	if err != nil {
		return false, fmt.Errorf("pipe template condition %q: %w", condExpr, err)
	}
	out, err := expr.Run(program, env)
	if err != nil {
		return false, fmt.Errorf("pipe template condition %q: %w", condExpr, err)
	}
	b, ok := out.(bool)
	if !ok {
		return false, fmt.Errorf("pipe template condition %q must evaluate to a boolean, got %T", condExpr, out)
	}
	return b, nil
}

// ---- tokenizer ----

type tagKind int

const (
	tkText tagKind = iota
	tkIf
	tkElif
	tkElse
	tkEnd
)

// token is a literal text run (tkText) or a {% %} control tag. expr holds the
// condition for tkIf/tkElif.
type token struct {
	kind tagKind
	text string
	expr string
}

func (k tagKind) String() string {
	switch k {
	case tkIf:
		return "{% if %}"
	case tkElif:
		return "{% elif %}"
	case tkElse:
		return "{% else %}"
	case tkEnd:
		return "{% end %}"
	default:
		return "text"
	}
}

// tokenizeTemplate scans sql into literal text runs and {% %} tags. It is a thin
// scanner: it locates {% ... %} spans and classifies the keyword; everything else
// is opaque text passed through verbatim.
func tokenizeTemplate(sql string) ([]token, error) {
	var toks []token
	for {
		idx := strings.Index(sql, "{%")
		if idx < 0 {
			if sql != "" {
				toks = append(toks, token{kind: tkText, text: sql})
			}
			return toks, nil
		}
		if idx > 0 {
			toks = append(toks, token{kind: tkText, text: sql[:idx]})
		}
		rest := sql[idx+2:]
		end := strings.Index(rest, "%}")
		if end < 0 {
			return nil, fmt.Errorf("pipe template: unterminated {%% tag (missing %%})")
		}
		tk, err := classifyTag(strings.TrimSpace(rest[:end]))
		if err != nil {
			return nil, err
		}
		toks = append(toks, tk)
		sql = rest[end+2:]
	}
}

// classifyTag turns the trimmed inside of a {% ... %} span into a token.
func classifyTag(inner string) (token, error) {
	if inner == "" {
		return token{}, fmt.Errorf("pipe template: empty {%% %%} tag")
	}
	kw, rest := splitFirst(inner) // splitFirst is defined in parser.go (same package)
	switch kw {
	case "if":
		if strings.TrimSpace(rest) == "" {
			return token{}, fmt.Errorf("pipe template: {%% if %%} requires a condition")
		}
		return token{kind: tkIf, expr: rest}, nil
	case "elif":
		if strings.TrimSpace(rest) == "" {
			return token{}, fmt.Errorf("pipe template: {%% elif %%} requires a condition")
		}
		return token{kind: tkElif, expr: rest}, nil
	case "else":
		if strings.TrimSpace(rest) != "" {
			return token{}, fmt.Errorf("pipe template: {%% else %%} takes no condition")
		}
		return token{kind: tkElse}, nil
	case "end":
		if strings.TrimSpace(rest) != "" {
			return token{}, fmt.Errorf("pipe template: {%% end %%} takes no arguments")
		}
		return token{kind: tkEnd}, nil
	default:
		return token{}, fmt.Errorf("pipe template: unknown tag {%% %s %%}", inner)
	}
}

// ---- parser (AST) ----
//
// ponytail: nesting is fully supported — {% if %} blocks may be nested inside any
// branch body, since parseSeq recurses on each nested {% if %}. There is no depth
// cap (Go's call stack bounds it; pathological inputs would stack-overflow long
// after they'd be unreadable).

type tmplNode interface{ isTmplNode() }

type textNode struct{ text string }

func (textNode) isTmplNode() {}

type condBranch struct {
	expr   string // condition; empty for the else branch
	isElse bool
	body   []tmplNode
}

type condNode struct{ branches []condBranch }

func (condNode) isTmplNode() {}

// parseTemplate parses the full token stream into a node sequence, rejecting any
// dangling elif/else/end that has no opening {% if %}.
func parseTemplate(toks []token) ([]tmplNode, error) {
	nodes, pos, err := parseSeq(toks, 0)
	if err != nil {
		return nil, err
	}
	if pos != len(toks) {
		return nil, fmt.Errorf("pipe template: unexpected %s without matching {%% if %%}", toks[pos].kind)
	}
	return nodes, nil
}

// parseSeq reads nodes until it hits a token that belongs to an enclosing if
// (elif/else/end) or the end of input; it returns the position of that token.
func parseSeq(toks []token, pos int) ([]tmplNode, int, error) {
	var out []tmplNode
	for pos < len(toks) {
		switch toks[pos].kind {
		case tkText:
			out = append(out, textNode{text: toks[pos].text})
			pos++
		case tkIf:
			cn, np, err := parseCond(toks, pos)
			if err != nil {
				return nil, pos, err
			}
			out = append(out, cn)
			pos = np
		default: // tkElif, tkElse, tkEnd close the current sequence
			return out, pos, nil
		}
	}
	return out, pos, nil
}

// parseCond parses one if/elif*/else?/end construct; toks[pos] must be tkIf.
func parseCond(toks []token, pos int) (condNode, int, error) {
	var cn condNode

	cn.branches = append(cn.branches, condBranch{expr: toks[pos].expr})
	pos++
	body, pos, err := parseSeq(toks, pos)
	if err != nil {
		return cn, pos, err
	}
	cn.branches[len(cn.branches)-1].body = body

	for pos < len(toks) && toks[pos].kind == tkElif {
		e := toks[pos].expr
		pos++
		body, pos, err = parseSeq(toks, pos)
		if err != nil {
			return cn, pos, err
		}
		cn.branches = append(cn.branches, condBranch{expr: e, body: body})
	}

	if pos < len(toks) && toks[pos].kind == tkElse {
		pos++
		body, pos, err = parseSeq(toks, pos)
		if err != nil {
			return cn, pos, err
		}
		cn.branches = append(cn.branches, condBranch{isElse: true, body: body})
	}

	if pos >= len(toks) || toks[pos].kind != tkEnd {
		return cn, pos, fmt.Errorf("pipe template: {%% if %%} is missing its {%% end %%}")
	}
	pos++ // consume {% end %}
	return cn, pos, nil
}

// renderTemplate walks the AST, emitting the literal text of each taken branch.
// The first branch whose condition is true wins; the else branch (if any) is the
// fallthrough. Non-taken branches contribute no text, dropping their placeholders.
func renderTemplate(nodes []tmplNode, env map[string]any) (string, error) {
	var sb strings.Builder
	if err := renderInto(&sb, nodes, env); err != nil {
		return "", err
	}
	return sb.String(), nil
}

func renderInto(sb *strings.Builder, nodes []tmplNode, env map[string]any) error {
	for _, n := range nodes {
		switch x := n.(type) {
		case textNode:
			sb.WriteString(x.text)
		case condNode:
			for _, br := range x.branches {
				take := br.isElse
				if !br.isElse {
					b, err := evalCondition(br.expr, env)
					if err != nil {
						return err
					}
					take = b
				}
				if take {
					if err := renderInto(sb, br.body, env); err != nil {
						return err
					}
					break
				}
			}
		}
	}
	return nil
}
