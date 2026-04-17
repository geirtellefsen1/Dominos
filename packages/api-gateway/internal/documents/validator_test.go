package documents

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// compileInline compiles a JSON schema from a literal string for test use.
// This mirrors what Validator.load does internally, but without a live store.
func compileInline(t *testing.T, id, src string) *jsonschema.Schema {
	t.Helper()
	c := jsonschema.NewCompiler()
	if err := c.AddResource(id, bytes.NewReader([]byte(src))); err != nil {
		t.Fatalf("add resource: %v", err)
	}
	s, err := c.Compile(id)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return s
}

func TestFlattenSurfacesPropertyErrors(t *testing.T) {
	schema := compileInline(t, "schema://t", `{
        "type": "object",
        "required": ["x"],
        "additionalProperties": false,
        "properties": { "x": { "type": "integer" } }
    }`)

	var doc any
	if err := json.Unmarshal([]byte(`{"x":"nope"}`), &doc); err != nil {
		t.Fatal(err)
	}
	err := schema.Validate(doc)
	if err == nil {
		t.Fatal("expected validation error")
	}
	ve, ok := err.(*jsonschema.ValidationError)
	if !ok {
		t.Fatalf("expected *jsonschema.ValidationError, got %T", err)
	}
	issues := flatten(ve)
	if len(issues) == 0 {
		t.Fatal("expected at least one issue from flatten()")
	}
	joined := strings.ToLower(strings.Join(messages(issues), "|"))
	if !strings.Contains(joined, "integer") && !strings.Contains(joined, "string") {
		t.Fatalf("expected type mention, got %q", joined)
	}
}

func TestFlattenCatchesMissingRequired(t *testing.T) {
	schema := compileInline(t, "schema://r", `{
        "type": "object",
        "required": ["needed"],
        "properties": { "needed": { "type": "string" } }
    }`)

	var doc any
	if err := json.Unmarshal([]byte(`{}`), &doc); err != nil {
		t.Fatal(err)
	}
	err := schema.Validate(doc)
	if err == nil {
		t.Fatal("expected validation error for missing required")
	}
	ve := err.(*jsonschema.ValidationError)
	issues := flatten(ve)
	if len(issues) == 0 {
		t.Fatal("expected issues")
	}
}

func messages(issues []ValidationIssue) []string {
	out := make([]string, 0, len(issues))
	for _, i := range issues {
		out = append(out, i.Message)
	}
	return out
}
