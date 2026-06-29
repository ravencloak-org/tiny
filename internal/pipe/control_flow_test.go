package pipe

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/tinyraven/tinyraven/internal/model"
)

// --- resolveControlFlow: branch selection + defined() ---

func TestResolveControlFlow_Selection(t *testing.T) {
	declared := []model.Param{
		{Name: "user_id", Type: "String"},
		{Name: "granularity", Type: "String"},
	}
	cases := []struct {
		name    string
		sql     string
		params  url.Values
		want    string
		wantErr bool
	}{
		{
			name:   "defined includes fragment",
			sql:    "SELECT 1 {% if defined(user_id) %}AND uid = {{String(user_id)}}{% end %}",
			params: url.Values{"user_id": {"u1"}},
			want:   "SELECT 1 AND uid = {{String(user_id)}}",
		},
		{
			name:   "undefined drops fragment and its placeholder",
			sql:    "SELECT 1 {% if defined(user_id) %}AND uid = {{String(user_id)}}{% end %}",
			params: url.Values{},
			want:   "SELECT 1 ",
		},
		{
			name:   "empty value counts as not defined",
			sql:    "x{% if defined(user_id) %}Y{% end %}",
			params: url.Values{"user_id": {""}},
			want:   "x",
		},
		{
			name:   "if branch via comparison",
			sql:    "{% if granularity == 'day' %}DAY{% elif granularity == 'month' %}MONTH{% else %}OTHER{% end %}",
			params: url.Values{"granularity": {"day"}},
			want:   "DAY",
		},
		{
			name:   "elif branch",
			sql:    "{% if granularity == 'day' %}DAY{% elif granularity == 'month' %}MONTH{% else %}OTHER{% end %}",
			params: url.Values{"granularity": {"month"}},
			want:   "MONTH",
		},
		{
			name:   "else branch when no match",
			sql:    "{% if granularity == 'day' %}DAY{% elif granularity == 'month' %}MONTH{% else %}OTHER{% end %}",
			params: url.Values{"granularity": {"week"}},
			want:   "OTHER",
		},
		{
			name:   "else branch when param absent",
			sql:    "{% if granularity == 'day' %}DAY{% else %}OTHER{% end %}",
			params: url.Values{},
			want:   "OTHER",
		},
		{
			name:   "no else, false condition yields empty",
			sql:    "A{% if granularity == 'day' %}B{% end %}C",
			params: url.Values{},
			want:   "AC",
		},
		{
			name:   "nested if inside taken branch",
			sql:    "{% if defined(user_id) %}U{% if granularity == 'day' %}D{% end %}{% end %}",
			params: url.Values{"user_id": {"u1"}, "granularity": {"day"}},
			want:   "UD",
		},
		{
			name:   "nested if inside dropped branch is skipped",
			sql:    "{% if defined(user_id) %}U{% if granularity == 'day' %}D{% end %}{% end %}TAIL",
			params: url.Values{"granularity": {"day"}}, // user_id absent -> whole outer block dropped
			want:   "TAIL",
		},
		{
			name:   "no control flow is a passthrough",
			sql:    "SELECT {{String(user_id)}}",
			params: url.Values{},
			want:   "SELECT {{String(user_id)}}",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := resolveControlFlow(c.sql, c.params, declared)
			if c.wantErr {
				if err == nil {
					t.Fatalf("want error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveControlFlow: %v", err)
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestResolveControlFlow_DroppedBranchParamNotSurfaced asserts the value param of a
// non-taken branch disappears entirely, so paramNamesInSQL does not list it.
func TestResolveControlFlow_DroppedBranchParamNotSurfaced(t *testing.T) {
	declared := []model.Param{{Name: "user_id", Type: "String"}}
	sql := "SELECT 1 {% if defined(user_id) %}AND uid = {{String(user_id)}}{% end %}"

	out, err := resolveControlFlow(sql, url.Values{}, declared)
	if err != nil {
		t.Fatalf("resolveControlFlow: %v", err)
	}
	if names := paramNamesInSQL(out); names["user_id"] {
		t.Errorf("user_id must not survive in a dropped branch; got names=%v sql=%q", names, out)
	}

	out, err = resolveControlFlow(sql, url.Values{"user_id": {"u1"}}, declared)
	if err != nil {
		t.Fatalf("resolveControlFlow: %v", err)
	}
	if names := paramNamesInSQL(out); !names["user_id"] {
		t.Errorf("user_id must survive when branch is taken; got names=%v sql=%q", names, out)
	}
}

// --- malformed templates: clear error, never a panic ---

func TestResolveControlFlow_Errors(t *testing.T) {
	declared := []model.Param{{Name: "x", Type: "String"}}
	cases := []struct {
		name, sql string
	}{
		{"missing end", "{% if defined(x) %}A"},
		{"dangling end", "A{% end %}"},
		{"dangling else", "A{% else %}B"},
		{"dangling elif", "A{% elif defined(x) %}B"},
		{"if without condition", "{% if %}A{% end %}"},
		{"unknown tag", "{% foreach x %}A{% end %}"},
		{"unterminated tag", "{% if defined(x) %A"},
		{"bad expression syntax", "{% if defined(x %}A{% end %}"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Must return an error and must not panic.
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panicked instead of erroring: %v", r)
				}
			}()
			out, err := resolveControlFlow(c.sql, url.Values{}, declared)
			if err == nil {
				t.Fatalf("want error, got %q", out)
			}
			if !strings.Contains(err.Error(), "pipe template") {
				t.Errorf("error should mention 'pipe template', got %v", err)
			}
		})
	}
}

// --- executor end-to-end: control flow + injection-safe binding ---

// TestRun_ControlFlow_OptionalParamNotRequired is the key injection-safety property
// (ADR 0003): a required-typed param referenced only inside a {% if defined %} block
// must NOT trigger a 400 when the request omits it — the branch is simply dropped.
func TestRun_ControlFlow_OptionalParamNotRequired(t *testing.T) {
	raw := "NODE endpoint\nSQL >\n    SELECT * FROM events WHERE 1=1{% if defined(user_id) %} AND user_id = {{String(user_id)}}{% end %}\nTYPE endpoint"

	t.Run("omitted: no 400, no placeholder bound", func(t *testing.T) {
		ch := &fakeCH{body: []byte("{}")}
		e := newExec(ch, mustParse(t, "p", raw))

		_, status, err := e.Run(context.Background(), "p", url.Values{})
		if err != nil || status != http.StatusOK {
			t.Fatalf("status=%d err=%v, want 200 (optional param omitted)", status, err)
		}
		if ch.calls != 1 {
			t.Fatalf("ClickHouse should be queried once, got %d", ch.calls)
		}
		if strings.Contains(ch.gotSQL, "user_id") {
			t.Errorf("dropped branch leaked user_id into SQL:\n%s", ch.gotSQL)
		}
		if _, bound := ch.gotParams["param_user_id"]; bound {
			t.Errorf("param_user_id must not be bound when branch dropped: %+v", ch.gotParams)
		}
	})

	t.Run("supplied: branch kept and bound safely", func(t *testing.T) {
		ch := &fakeCH{body: []byte("{}")}
		e := newExec(ch, mustParse(t, "p", raw))

		_, status, err := e.Run(context.Background(), "p", url.Values{"user_id": {"u1"}})
		if err != nil || status != http.StatusOK {
			t.Fatalf("status=%d err=%v, want 200", status, err)
		}
		if !strings.Contains(ch.gotSQL, "{user_id:String}") {
			t.Errorf("surviving placeholder not rewritten to CH param:\n%s", ch.gotSQL)
		}
		if strings.Contains(ch.gotSQL, "{{") {
			t.Errorf("raw {{...}} left in SQL:\n%s", ch.gotSQL)
		}
		if ch.gotParams["param_user_id"] != "u1" {
			t.Errorf("param_user_id = %q, want u1", ch.gotParams["param_user_id"])
		}
	})
}

// TestRun_ControlFlow_ComparisonBranch checks a comparison condition picks the right
// SQL and that only the surviving branch's params are bound.
func TestRun_ControlFlow_ComparisonBranch(t *testing.T) {
	raw := "NODE endpoint\nSQL >\n    SELECT {% if granularity == 'day' %}toDate(ts){% else %}toStartOfMonth(ts){% end %} d FROM events\nTYPE endpoint"
	ch := &fakeCH{body: []byte("{}")}
	e := newExec(ch, mustParse(t, "p", raw))

	if _, status, err := e.Run(context.Background(), "p", url.Values{"granularity": {"day"}}); err != nil || status != http.StatusOK {
		t.Fatalf("status=%d err=%v", status, err)
	}
	if !strings.Contains(ch.gotSQL, "toDate(ts)") || strings.Contains(ch.gotSQL, "toStartOfMonth") {
		t.Errorf("day branch not selected:\n%s", ch.gotSQL)
	}
}

// TestRun_ControlFlow_MalformedIs400 asserts a malformed template fails the request
// with a 400 and never reaches ClickHouse.
func TestRun_ControlFlow_MalformedIs400(t *testing.T) {
	raw := "NODE endpoint\nSQL >\n    SELECT 1 {% if defined(user_id) %} AND x=1\nTYPE endpoint"
	ch := &fakeCH{body: []byte("{}")}
	e := newExec(ch, mustParse(t, "p", raw))

	_, status, err := e.Run(context.Background(), "p", url.Values{})
	if status != http.StatusBadRequest || err == nil {
		t.Fatalf("status=%d err=%v, want 400 for malformed template", status, err)
	}
	if ch.calls != 0 {
		t.Errorf("ClickHouse must not be queried on malformed template (calls=%d)", ch.calls)
	}
}
