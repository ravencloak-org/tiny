// Package datasource parses .datasource project files into model.Datasource
// values and persists them in the Redis-backed metadata registry (ADR 0001).
// The file format mirrors Tinybird's .datasource files byte-for-byte: a SCHEMA
// block of indented column lines followed by ENGINE/ENGINE_*/CONNECTOR
// directives (ADRs 0008, 0027).
package datasource

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/tinyraven/tinyraven/internal/model"
)

// engineKeyExprs are the ENGINE_* options whose values are checked for
// references to undefined SCHEMA columns (ADR 0027). ClickHouse owns semantic
// and type validation; we only do structural + referential checks.
var engineKeyExprs = []string{"ENGINE_SORTING_KEY", "ENGINE_PARTITION_KEY", "ENGINE_TTL"}

// identRe matches bare identifiers for best-effort column-reference checks.
var identRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// exprKeywords are SQL words that appear in ENGINE_* expressions but are never
// column names; skipped so referential validation does not false-positive.
// ponytail: deliberately small denylist — function calls are excluded by the
// "followed by (" rule, and ClickHouse is the real validator at deploy time.
var exprKeywords = map[string]bool{
	"interval": true, "second": true, "seconds": true, "minute": true, "minutes": true,
	"hour": true, "hours": true, "day": true, "days": true, "week": true, "weeks": true,
	"month": true, "months": true, "quarter": true, "quarters": true, "year": true, "years": true,
	"and": true, "or": true, "not": true, "null": true, "asc": true, "desc": true,
	"to": true, "by": true, "true": true, "false": true, "nan": true, "inf": true,
}

// ParseFile reads a .datasource file and parses it. The datasource name is the
// file basename without the .datasource extension.
func ParseFile(path string) (*model.Datasource, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSuffix(filepath.Base(path), ".datasource")
	return Parse(name, string(raw))
}

// Parse parses raw .datasource text into a model.Datasource and runs structural
// + referential validation (ADR 0027). It does NOT validate ClickHouse types.
func Parse(name, raw string) (*model.Datasource, error) {
	var (
		cols      []model.Column
		engine    string
		opts      = map[string]string{}
		connector string
		inSchema  bool
	)

	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)

		if inSchema {
			switch {
			case trimmed == "":
				continue // tolerate blank lines inside the SCHEMA block
			case isIndented(line):
				if c := parseColumn(trimmed); c.Name != "" {
					cols = append(cols, c)
				}
				continue
			default:
				// A non-indented, non-blank line ends the SCHEMA block; fall
				// through so this line is handled as a top-level directive.
				inSchema = false
			}
		}

		if trimmed == "" {
			continue
		}
		first, rest := splitFirst(trimmed)
		upper := strings.ToUpper(first)
		switch {
		case upper == "SCHEMA":
			inSchema = true
		case upper == "ENGINE":
			engine = unquote(strings.TrimSpace(rest))
		case strings.HasPrefix(upper, "ENGINE_"):
			opts[upper] = unquote(strings.TrimSpace(rest))
		case upper == "CONNECTOR":
			connector = unquote(strings.TrimSpace(rest))
		default:
			// ponytail: unknown directives (DESCRIPTION, TAGS, TOKEN, ...) are
			// ignored in MVP — they carry no meaning for ingest/query yet.
		}
	}

	if engine == "" {
		engine = "MergeTree" // ADR 0008 default when ENGINE omitted
	}

	ds := &model.Datasource{
		Name:       name,
		Schema:     cols,
		Engine:     engine,
		EngineOpts: opts,
		Connector:  connector,
		Raw:        raw,
	}
	if err := validate(ds); err != nil {
		return nil, err
	}
	return ds, nil
}

// validate enforces ADR 0027 structural + referential rules.
func validate(ds *model.Datasource) error {
	var problems []string
	if len(ds.Schema) == 0 {
		problems = append(problems, "SCHEMA is required and must define at least one column")
	}

	schema := make(map[string]bool, len(ds.Schema))
	for _, c := range ds.Schema {
		schema[c.Name] = true
	}
	for _, key := range engineKeyExprs {
		expr, ok := ds.EngineOpts[key]
		if !ok {
			continue
		}
		for _, id := range columnIdents(expr) {
			if !schema[id] {
				problems = append(problems, fmt.Sprintf("%s references unknown column %q", key, id))
			}
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid datasource %q: %s", ds.Name, strings.Join(problems, "; "))
	}
	return nil
}

// columnIdents extracts the bare identifiers in an ENGINE_* expression that
// should refer to SCHEMA columns: function names (identifier followed by "(")
// and known SQL keywords are excluded. De-duplicated, first-seen order.
func columnIdents(expr string) []string {
	var (
		out  []string
		seen = map[string]bool{}
	)
	for _, loc := range identRe.FindAllStringIndex(expr, -1) {
		word := expr[loc[0]:loc[1]]
		if strings.HasPrefix(strings.TrimLeft(expr[loc[1]:], " \t"), "(") {
			continue // function call, not a column
		}
		if exprKeywords[strings.ToLower(word)] || seen[word] {
			continue
		}
		seen[word] = true
		out = append(out, word)
	}
	return out
}

// parseColumn parses one SCHEMA line into a Column. It splits "name type" on the
// first whitespace, tolerates a trailing comma, and drops Tinybird `json:$.path`
// jsonpath annotations (ignored in MVP).
func parseColumn(line string) model.Column {
	s := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), ","))
	if s == "" {
		return model.Column{}
	}
	sp := strings.IndexFunc(s, unicode.IsSpace)
	if sp < 0 {
		return model.Column{Name: strings.Trim(s, "`")} // name only, type left to CH
	}
	name := strings.Trim(s[:sp], "`")
	typ := stripJSONPath(strings.TrimSpace(s[sp+1:]))
	return model.Column{Name: name, Type: typ}
}

// stripJSONPath removes trailing `json:` / “ `json:...` “ annotations while
// preserving ClickHouse types that contain spaces, e.g. "Map(String, String)".
func stripJSONPath(typ string) string {
	fields := strings.Fields(typ)
	kept := fields[:0]
	for _, f := range fields {
		l := strings.ToLower(f)
		if strings.HasPrefix(l, "json:") || strings.HasPrefix(l, "`json:") {
			continue
		}
		kept = append(kept, f)
	}
	return strings.Join(kept, " ")
}

// splitFirst splits s into its first whitespace-delimited token and the rest.
func splitFirst(s string) (first, rest string) {
	sp := strings.IndexFunc(s, unicode.IsSpace)
	if sp < 0 {
		return s, ""
	}
	return s[:sp], strings.TrimSpace(s[sp+1:])
}

// unquote strips a single matched pair of surrounding double or single quotes.
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func isIndented(line string) bool {
	return len(line) > 0 && (line[0] == ' ' || line[0] == '\t')
}
