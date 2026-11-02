// Package ows validates Open Workout Schema documents against the schemas in this repository.
package ows

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Kinds are the document kinds, by file name under schema/v0.
var Kinds = []string{"workout", "program"}

// Validator holds the compiled schemas, keyed by their $id.
type Validator struct {
	byID   map[string]*jsonschema.Schema
	byKind map[string]*jsonschema.Schema
}

// NewValidator compiles every schema under root/schema/v0.
func NewValidator(root string) (*Validator, error) {
	v := &Validator{byID: map[string]*jsonschema.Schema{}, byKind: map[string]*jsonschema.Schema{}}
	c := jsonschema.NewCompiler()
	for _, kind := range Kinds {
		path := filepath.Join(root, "schema", "v0", kind+".json")
		s, err := c.Compile(path)
		if err != nil {
			return nil, fmt.Errorf("compile %s: %w", path, err)
		}
		v.byKind[kind] = s
		v.byID[s.ID] = s
	}
	return v, nil
}

// IDs lists the schema $ids the validator knows.
func (v *Validator) IDs() []string {
	ids := make([]string, 0, len(v.byID))
	for id := range v.byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Validate returns every problem in one document. The document says which schema it follows in
// $schema; without one, it is tried as a workout.
func (v *Validator) Validate(data []byte) []string {
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		return []string{"invalid JSON: " + err.Error()}
	}
	schema := v.byKind["workout"]
	if obj, ok := doc.(map[string]any); ok {
		if id, ok := obj["$schema"].(string); ok {
			s, known := v.byID[id]
			if !known {
				return []string{fmt.Sprintf("unknown $schema %q; this validator knows %v", id, v.IDs())}
			}
			schema = s
		}
	}
	if err := schema.Validate(doc); err != nil {
		var ve *jsonschema.ValidationError
		if errors.As(err, &ve) {
			return []string{ve.Error()}
		}
		return []string{err.Error()}
	}
	return nil
}
