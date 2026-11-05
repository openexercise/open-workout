package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"

	openworkout "github.com/openexercise/open-workout"
)

// VectorTarget is what an implementation of the progression rules works out for a case: the
// runner contract's output. Load is the next weight in the case's unit, or null for none; Loads
// is each set group's weight when the slot's groups carry their own percentage; Sets is the set
// scheme to do next, for a stages rule; Exercise is the exercise to do next, for a variations
// rule; NeedsStart says a starting number is missing. Explanations aren't part of it.
type VectorTarget struct {
	Load       *float64        `json:"load"`
	Loads      []float64       `json:"loads,omitempty"`
	Sets       json.RawMessage `json:"sets,omitempty"`
	Exercise   string          `json:"exercise,omitempty"`
	NeedsStart bool            `json:"needs_start"`
}

// VectorCase is one published case (schema/v0/progression-vectors.json). Input is the case as
// an implementation receives it: everything but want.
type VectorCase struct {
	Name  string
	Input json.RawMessage
	Want  VectorTarget
}

// ProgressionVectors reads the published cases.
func ProgressionVectors() ([]VectorCase, error) {
	raw, err := openworkout.Schemas.ReadFile("schema/v0/progression-vectors.json")
	if err != nil {
		return nil, err
	}
	var doc struct {
		Cases []map[string]json.RawMessage `json:"cases"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("progression vectors: %w", err)
	}
	out := make([]VectorCase, 0, len(doc.Cases))
	for i, c := range doc.Cases {
		var v VectorCase
		if err := json.Unmarshal(c["name"], &v.Name); err != nil {
			return nil, fmt.Errorf("progression vectors: case %d has no name", i)
		}
		if err := json.Unmarshal(c["want"], &v.Want); err != nil {
			return nil, fmt.Errorf("progression vectors: %s: want: %w", v.Name, err)
		}
		delete(c, "want")
		if v.Input, err = json.Marshal(c); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// VectorReport is every case's outcome, in order.
type VectorReport struct {
	Results []Result `json:"results"`
}

// Passed reports whether every case passed.
func (r VectorReport) Passed() bool {
	return len(r.Results) > 0 && !slices.ContainsFunc(r.Results, func(res Result) bool { return res.Status != Pass })
}

// CheckVectors runs every published case through compute, an implementation of the progression
// rules, and compares what it works out with the case's want. A server's own tests can call it
// with their evaluator; ows vectors calls it with a command (the runner contract).
func CheckVectors(ctx context.Context, compute func(ctx context.Context, input json.RawMessage) (VectorTarget, error)) (VectorReport, error) {
	cases, err := ProgressionVectors()
	if err != nil {
		return VectorReport{}, err
	}
	var report VectorReport
	for _, c := range cases {
		res := Result{Name: c.Name, Status: Pass}
		got, err := compute(ctx, c.Input)
		if err == nil {
			err = c.Want.compare(got)
		}
		if err != nil {
			res.Status, res.Detail = Fail, err.Error()
		}
		report.Results = append(report.Results, res)
	}
	return report, nil
}

// compare says how got differs from want. Only what want states is compared, beyond the load
// and needs_start, which every case states.
func (want VectorTarget) compare(got VectorTarget) error {
	switch {
	case want.Load == nil && got.Load != nil:
		return fmt.Errorf("load = %g, want none", *got.Load)
	case want.Load != nil && got.Load == nil:
		return fmt.Errorf("load = none, want %g", *want.Load)
	case want.Load != nil && !same(*want.Load, *got.Load):
		return fmt.Errorf("load = %g, want %g", *got.Load, *want.Load)
	case want.NeedsStart != got.NeedsStart:
		return fmt.Errorf("needs_start = %v, want %v", got.NeedsStart, want.NeedsStart)
	case want.Exercise != "" && got.Exercise != want.Exercise:
		return fmt.Errorf("exercise = %q, want %q", got.Exercise, want.Exercise)
	}
	if want.Loads != nil {
		if len(got.Loads) != len(want.Loads) {
			return fmt.Errorf("loads = %v, want %v", got.Loads, want.Loads)
		}
		for i := range want.Loads {
			if !same(want.Loads[i], got.Loads[i]) {
				return fmt.Errorf("loads = %v, want %v", got.Loads, want.Loads)
			}
		}
	}
	if want.Sets != nil {
		var w, g any
		if err := json.Unmarshal(want.Sets, &w); err != nil {
			return err
		}
		if len(bytes.TrimSpace(got.Sets)) == 0 || json.Unmarshal(got.Sets, &g) != nil || !reflect.DeepEqual(w, g) {
			return fmt.Errorf("sets = %s, want %s", clip(got.Sets), clip(want.Sets))
		}
	}
	return nil
}

func same(a, b float64) bool { return math.Abs(a-b) < 1e-9 }
