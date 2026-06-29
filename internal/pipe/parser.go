// Package pipe parses .pipe project files into model.Pipe values, stores them
// in a hot-swappable in-memory registry (ADR 0020), and executes published
// endpoints against ClickHouse (ADRs 0003, 0009, 0012). Pipes are sequences of
// NODE blocks; the terminal node (TYPE endpoint, or the last node) is the
// published endpoint, with earlier nodes composed in as CTEs at query time.
package pipe

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/tinyraven/tinyraven/internal/model"
)

// placeholderRe matches Tinybird value templates: {{Type(name)}} and
// {{Type(name, default)}} (ADR 0003, MVP value-params only, no control flow).
// Groups: 1=Type, 2=name, 3=",default" segment (presence => HasDefault),
// 4=default value. It is the single source of truth for both extraction
// (parser) and rewrite-to-ClickHouse-params (executor).
var placeholderRe = regexp.MustCompile(`\{\{\s*(\w+)\s*\(\s*(\w+)\s*(,\s*(.*?)\s*)?\)\s*\}\}`)

// block is one NODE/ENDPOINT/MATERIALIZATION section while parsing.
type block struct {
	kind        string // "node", "endpoint", or "materialization"
	name        string
	sql         string
	typ         string // TYPE directive value, lowercased
	rateLimit   int
	cacheTTL    int
	targetTable string
}

// ParseFile reads a .pipe file and parses it. Pipe.Name is the file basename
// without the .pipe extension.
func ParseFile(path string) (*model.Pipe, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSuffix(filepath.Base(path), ".pipe")
	return Parse(name, string(raw))
}

// Parse parses raw .pipe text into a model.Pipe.
func Parse(name, raw string) (*model.Pipe, error) {
	blocks, err := parseBlocks(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid pipe %q: %w", name, err)
	}
	if len(blocks) == 0 {
		return nil, fmt.Errorf("invalid pipe %q: no NODE/ENDPOINT/MATERIALIZATION blocks", name)
	}

	term := terminalIndex(blocks)

	p := &model.Pipe{Name: name, Raw: raw}
	for i, b := range blocks {
		if i == term {
			continue // the terminal block becomes Endpoint/Material, not a Node
		}
		if b.kind == "node" {
			p.Nodes = append(p.Nodes, model.Node{Name: b.name, SQL: b.sql})
		}
	}

	tb := blocks[term]
	if tb.kind == "materialization" || tb.typ == "materialization" {
		p.Material = &model.Materialization{
			Name:        tb.name,
			TargetTable: tb.targetTable,
			SQL:         tb.sql,
		}
		// ponytail: materializations take no runtime params in MVP, so we skip
		// placeholder extraction for them.
	} else {
		p.Endpoint = &model.Endpoint{
			Name:      tb.name,
			SQL:       tb.sql,
			RateLimit: tb.rateLimit,
			CacheTTL:  tb.cacheTTL,
			Params:    extractParams(p.Nodes, tb.sql),
		}
	}
	return p, nil
}

// terminalIndex picks the published block: the last block explicitly marked as
// an output (ENDPOINT/MATERIALIZATION header, or TYPE endpoint/query/
// materialization); failing that, the last NODE block.
func terminalIndex(blocks []block) int {
	term := -1
	for i, b := range blocks {
		switch {
		case b.kind == "endpoint" || b.kind == "materialization":
			term = i
		case b.typ == "endpoint" || b.typ == "query" || b.typ == "materialization":
			term = i
		}
	}
	if term >= 0 {
		return term
	}
	for i := len(blocks) - 1; i >= 0; i-- {
		if blocks[i].kind == "node" {
			return i
		}
	}
	return len(blocks) - 1
}

// extractParams scans node SQL (in order) then endpoint SQL for {{Type(name)}}
// placeholders, de-duplicating by name and preserving first-seen order. The
// composed query binds all of them, so upstream node params must be collected
// too (ADR 0003).
func extractParams(nodes []model.Node, endpointSQL string) []model.Param {
	var (
		out  []model.Param
		seen = map[string]bool{}
	)
	collect := func(sql string) {
		for _, m := range placeholderRe.FindAllStringSubmatch(sql, -1) {
			name := m[2]
			if seen[name] {
				continue
			}
			seen[name] = true
			p := model.Param{Name: name, Type: model.ParamType(m[1])}
			if m[3] != "" { // the ",default" segment participated
				p.HasDefault = true
				p.Default = unquote(strings.TrimSpace(m[4]))
			}
			out = append(out, p)
		}
	}
	for _, n := range nodes {
		collect(n.SQL)
	}
	collect(endpointSQL)
	return out
}

// parseBlocks splits raw .pipe text into ordered blocks. Block headers (NODE/
// ENDPOINT/MATERIALIZATION) and directives (TYPE/RATE_LIMIT/CACHE_TTL/
// TARGET_TABLE/SQL) are non-indented; SQL block bodies are indented.
func parseBlocks(raw string) ([]block, error) {
	var (
		blocks  []block
		cur     *block
		inSQL   bool
		sqlBody []string
	)

	flushSQL := func() {
		if cur != nil && inSQL {
			cur.sql = strings.TrimSpace(strings.Join(sqlBody, "\n"))
		}
		inSQL = false
		sqlBody = nil
	}

	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)

		// Inside a SQL > block, indented (or blank) lines are body text. A
		// non-indented, non-blank line ends the body and is reprocessed below.
		if inSQL {
			if trimmed == "" || isIndented(line) {
				sqlBody = append(sqlBody, line)
				continue
			}
			flushSQL()
		}

		if trimmed == "" {
			continue
		}

		first, rest := splitFirst(trimmed)
		switch strings.ToUpper(first) {
		case "NODE", "ENDPOINT", "MATERIALIZATION":
			if cur != nil {
				blocks = append(blocks, *cur)
			}
			name := strings.TrimSpace(rest)
			if name == "" {
				return nil, fmt.Errorf("%s block is missing a name", strings.ToUpper(first))
			}
			cur = &block{kind: strings.ToLower(first), name: name}
		case "SQL":
			if cur == nil {
				return nil, fmt.Errorf("SQL directive before any NODE/ENDPOINT block")
			}
			body := strings.TrimSpace(rest)
			if body == ">" || body == "" {
				inSQL, sqlBody = true, nil // block form: collect indented lines
			} else {
				cur.sql = strings.TrimSpace(strings.TrimPrefix(body, ">")) // inline form
			}
		case "TYPE":
			if cur != nil {
				cur.typ = strings.ToLower(strings.TrimSpace(rest))
			}
		case "RATE_LIMIT":
			if cur != nil {
				cur.rateLimit = atoiSafe(rest)
			}
		case "CACHE_TTL":
			if cur != nil {
				cur.cacheTTL = atoiSafe(rest)
			}
		case "TARGET_TABLE":
			if cur != nil {
				cur.targetTable = strings.TrimSpace(rest)
			}
		default:
			// ponytail: unknown directives (DESCRIPTION, TAGS, TOKEN, ...) are
			// ignored in MVP.
		}
	}
	flushSQL()
	if cur != nil {
		blocks = append(blocks, *cur)
	}
	return blocks, nil
}

// splitFirst splits s into its first whitespace-delimited token and the rest.
func splitFirst(s string) (first, rest string) {
	sp := strings.IndexFunc(s, unicode.IsSpace)
	if sp < 0 {
		return s, ""
	}
	return s[:sp], strings.TrimSpace(s[sp+1:])
}

// unquote strips a single matched pair of surrounding double or single quotes,
// so a template default like 'rock' binds as the string rock.
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// atoiSafe parses a leading integer, returning 0 on any malformed value.
func atoiSafe(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

func isIndented(line string) bool {
	return len(line) > 0 && (line[0] == ' ' || line[0] == '\t')
}
