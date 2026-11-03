// Package ows validates Open Workout Schema documents against the schemas in this repository.
package ows

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
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
	return NewValidatorFS(os.DirFS(root))
}

// NewValidatorFS compiles every schema under schema/v0 in fsys: a checkout, or the module's
// embedded copy (openworkout.Schemas). Each is registered under its $id, so references between
// them resolve without touching the network.
func NewValidatorFS(fsys fs.FS) (*Validator, error) {
	c := jsonschema.NewCompiler()
	ids := map[string]string{}
	for _, kind := range Kinds {
		name := path.Join("schema", "v0", kind+".json")
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		id, _ := doc.(map[string]any)["$id"].(string)
		if id == "" {
			return nil, fmt.Errorf("%s has no $id", name)
		}
		if err := c.AddResource(id, doc); err != nil {
			return nil, fmt.Errorf("add %s: %w", name, err)
		}
		ids[kind] = id
	}
	v := &Validator{byID: map[string]*jsonschema.Schema{}, byKind: map[string]*jsonschema.Schema{}}
	for _, kind := range Kinds {
		s, err := c.Compile(ids[kind])
		if err != nil {
			return nil, fmt.Errorf("compile %s: %w", kind, err)
		}
		v.byKind[kind] = s
		v.byID[s.ID] = s
	}
	return v, nil
}

// ValidateAs returns every problem in a document of a kind ("workout", "program"), whatever its
// $schema says. A server's response is a workout because of where it came from.
func (v *Validator) ValidateAs(kind string, data []byte) []string {
	s, ok := v.byKind[kind]
	if !ok {
		return []string{fmt.Sprintf("unknown kind %q", kind)}
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return []string{"invalid JSON: " + err.Error()}
	}
	return problems(s, doc)
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
	return problems(schema, doc)
}

func problems(s *jsonschema.Schema, doc any) []string {
	if err := s.Validate(doc); err != nil {
		var ve *jsonschema.ValidationError
		if errors.As(err, &ve) {
			return []string{ve.Error()}
		}
		return []string{err.Error()}
	}
	return nil
}
