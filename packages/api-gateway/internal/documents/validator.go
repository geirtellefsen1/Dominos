package documents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// Validator compiles JSON schemas on demand and caches them.
type Validator struct {
	store *Store
	mu    sync.RWMutex
	cache map[string]*jsonschema.Schema
}

func NewValidator(store *Store) *Validator {
	return &Validator{store: store, cache: make(map[string]*jsonschema.Schema)}
}

func (v *Validator) load(ctx context.Context, schemaID string) (*jsonschema.Schema, error) {
	v.mu.RLock()
	if s, ok := v.cache[schemaID]; ok {
		v.mu.RUnlock()
		return s, nil
	}
	v.mu.RUnlock()

	raw, err := v.store.LoadSchema(ctx, schemaID)
	if err != nil {
		return nil, err
	}

	c := jsonschema.NewCompiler()
	resourceURL := "schema://" + schemaID
	if err := c.AddResource(resourceURL, bytes.NewReader(raw)); err != nil {
		return nil, fmt.Errorf("add resource: %w", err)
	}
	compiled, err := c.Compile(resourceURL)
	if err != nil {
		return nil, fmt.Errorf("compile schema %s: %w", schemaID, err)
	}

	v.mu.Lock()
	v.cache[schemaID] = compiled
	v.mu.Unlock()
	return compiled, nil
}

// ValidationIssue is a single flattened error suitable for a 400 response.
type ValidationIssue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

func (v *Validator) Validate(ctx context.Context, schemaID string, body json.RawMessage) ([]ValidationIssue, error) {
	schema, err := v.load(ctx, schemaID)
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return []ValidationIssue{{Path: "", Message: "body is not valid JSON: " + err.Error()}}, nil
	}
	if err := schema.Validate(decoded); err != nil {
		var ve *jsonschema.ValidationError
		if errors.As(err, &ve) {
			return flatten(ve), nil
		}
		return []ValidationIssue{{Path: "", Message: err.Error()}}, nil
	}
	return nil, nil
}

func flatten(ve *jsonschema.ValidationError) []ValidationIssue {
	var out []ValidationIssue
	var walk func(*jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			path := e.InstanceLocation
			if path == "" {
				path = "/"
			}
			msg := strings.TrimSpace(e.Message)
			out = append(out, ValidationIssue{Path: path, Message: msg})
			return
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(ve)
	return out
}

