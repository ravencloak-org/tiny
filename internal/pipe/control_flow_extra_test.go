package pipe

import (
	"net/url"
	"strings"
	"testing"

	"github.com/tinyraven/tinyraven/internal/model"
)

// classifyTag rejects malformed {% %} tags. These complement the cases in
// control_flow_test.go (if-without-condition, unknown tag, …) by covering the
// elif/else/end argument-shape branches and the empty tag.
func TestClassifyTag_ArgumentShapeErrors(t *testing.T) {
	declared := []model.Param{{Name: "x", Type: "String"}}
	cases := []struct {
		name, sql string
	}{
		{"empty tag", "A{%  %}B"},
		{"elif without condition", "{% if defined(x) %}A{% elif %}B{% end %}"},
		{"else with condition", "{% if defined(x) %}A{% else x %}B{% end %}"},
		{"end with arguments", "{% if defined(x) %}A{% end now %}"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panicked instead of erroring: %v", r)
				}
			}()
			if out, err := resolveControlFlow(c.sql, url.Values{}, declared); err == nil {
				t.Fatalf("want error, got %q", out)
			} else if !strings.Contains(err.Error(), "pipe template") {
				t.Errorf("error should mention 'pipe template', got %v", err)
			}
		})
	}
}

// A condition that compiles and runs but yields a non-boolean (here a bare
// string param) must surface a clear error rather than silently coercing — the
// injection-safety invariant that control flow only ever decides true/false.
func TestEvalCondition_NonBooleanResult(t *testing.T) {
	declared := []model.Param{{Name: "x", Type: "String"}}
	out, err := resolveControlFlow("{% if x %}A{% end %}", url.Values{"x": {"hello"}}, declared)
	if err == nil {
		t.Fatalf("non-boolean condition must error, got %q", out)
	}
	if !strings.Contains(err.Error(), "boolean") {
		t.Errorf("error should explain the boolean requirement, got %v", err)
	}
}

// Full if/elif/else selection: exactly one branch is emitted, and the chosen
// branch depends on the supplied param value.
func TestResolveControlFlow_ElifElseSelection(t *testing.T) {
	declared := []model.Param{{Name: "a", Type: "String"}}
	const sql = "{% if a == '1' %}A{% elif a == '2' %}B{% else %}C{% end %}"
	cases := map[string]string{"1": "A", "2": "B", "3": "C"}
	for in, want := range cases {
		t.Run("a="+in, func(t *testing.T) {
			got, err := resolveControlFlow(sql, url.Values{"a": {in}}, declared)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != want {
				t.Errorf("a=%q selected %q, want %q", in, got, want)
			}
		})
	}
}

// Nested {% if %} inside a branch body must parse and render correctly
// (parseSeq recursion), and the inner block only contributes when both its
// enclosing branch and its own condition are taken.
func TestResolveControlFlow_Nested(t *testing.T) {
	declared := []model.Param{{Name: "a", Type: "String"}, {Name: "b", Type: "String"}}
	const sql = "START {% if a == '1' %}X{% if b == '2' %}Y{% end %}Z{% end %} END"
	cases := []struct {
		a, b, want string
	}{
		{"1", "2", "START XYZ END"},
		{"1", "9", "START XZ END"},
		{"9", "2", "START  END"},
	}
	for _, c := range cases {
		t.Run("a="+c.a+"_b="+c.b, func(t *testing.T) {
			got, err := resolveControlFlow(sql, url.Values{"a": {c.a}, "b": {c.b}}, declared)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("a=%q b=%q -> %q, want %q", c.a, c.b, got, c.want)
			}
		})
	}
}
