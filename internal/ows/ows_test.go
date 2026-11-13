package ows

import (
	openworkout "github.com/openexercise/open-workout"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExamplesValidate(t *testing.T) {
	v, err := NewValidator("../..")
	if err != nil {
		t.Fatal(err)
	}
	paths, _ := filepath.Glob("../../examples/*.json")
	if len(paths) == 0 {
		t.Fatal("no examples")
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if problems := v.Validate(data); len(problems) > 0 {
			t.Errorf("%s: %v", p, problems)
		}
	}
}

func TestValidateRejects(t *testing.T) {
	v, err := NewValidator("../..")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ name, doc, want string }{
		{"not json", `{`, "invalid JSON"},
		{"unknown schema", `{"$schema": "https://example.com/x.json"}`, "unknown $schema"},
		{"workout missing source", `{"started_at": "2026-10-27T17:42:00-05:00", "exercises": []}`, "source"},
		{"workout with an invented field", `{"started_at": "2026-10-27T17:42:00-05:00", "source": {"kind": "app"}, "exercises": [], "calories": 300}`, "calories"},
		{"log without exported_at", `{"$schema": "https://openworkout.dev/schema/v0/log.json", "workouts": []}`, "exported_at"},
		{"log holding a bad workout", `{"$schema": "https://openworkout.dev/schema/v0/log.json", "exported_at": "2026-10-28T08:00:00Z", "workouts": [{"started_at": "2026-10-27T17:42:00-05:00", "exercises": []}]}`, "source"},
		{"routine row without a name", `{"$schema": "https://openworkout.dev/schema/v0/routine.json", "name": "Push A", "exercises": [{"exercise_id": "oec:dip"}]}`, "name"},
		{"program without weeks", `{"$schema": "https://openworkout.dev/schema/v0/program.json", "progression": "x"}`, "weeks"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problems := v.Validate([]byte(tt.doc))
			if len(problems) == 0 || !strings.Contains(strings.Join(problems, "\n"), tt.want) {
				t.Errorf("problems = %v, want one mentioning %q", problems, tt.want)
			}
		})
	}
}

// The embedded schemas are what an installed ows and the conformance suite use; they must compile
// and know every kind.
func TestEmbeddedSchemas(t *testing.T) {
	v, err := NewValidatorFS(openworkout.Schemas)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(v.IDs()); got != len(Kinds) {
		t.Errorf("%d schemas, want %d", got, len(Kinds))
	}
	for _, kind := range Kinds {
		if p := v.ValidateAs(kind, []byte(`{}`)); len(p) == 0 {
			t.Errorf("an empty %s validated", kind)
		}
	}
}
