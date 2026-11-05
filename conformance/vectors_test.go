package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestCheckVectors(t *testing.T) {
	cases, err := ProgressionVectors()
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) < 50 {
		t.Fatalf("%d cases; the published file has more", len(cases))
	}
	byInput := map[string]VectorTarget{}
	for _, c := range cases {
		if _, ok := byInput[string(c.Input)]; ok {
			t.Fatalf("two cases have the same input: %s", c.Name)
		}
		var in map[string]any
		if err := json.Unmarshal(c.Input, &in); err != nil || in["want"] != nil || in["slot"] == nil {
			t.Fatalf("%s: the input should be the case without want: %s", c.Name, c.Input)
		}
		byInput[string(c.Input)] = c.Want
	}
	ctx := context.Background()

	// An implementation that knows the answers passes.
	right := func(_ context.Context, in json.RawMessage) (VectorTarget, error) { return byInput[string(in)], nil }
	report, err := CheckVectors(ctx, right)
	if err != nil || !report.Passed() || len(report.Results) != len(cases) {
		t.Fatalf("a right implementation: %v %+v", err, report)
	}

	// Each kind of mistake fails the cases it touches.
	for name, wrong := range map[string]func(VectorTarget) VectorTarget{
		"load off by a step": func(w VectorTarget) VectorTarget {
			if w.Load != nil {
				l := *w.Load + 2.5
				w.Load = &l
			}
			return w
		},
		"needs_start flipped": func(w VectorTarget) VectorTarget { w.NeedsStart = !w.NeedsStart; return w },
		"no set scheme":       func(w VectorTarget) VectorTarget { w.Sets = nil; return w },
		"no exercise":         func(w VectorTarget) VectorTarget { w.Exercise = ""; return w },
	} {
		report, err := CheckVectors(ctx, func(_ context.Context, in json.RawMessage) (VectorTarget, error) {
			return wrong(byInput[string(in)]), nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if report.Passed() {
			t.Errorf("%s: passed", name)
		}
	}
	report, _ = CheckVectors(ctx, func(context.Context, json.RawMessage) (VectorTarget, error) {
		return VectorTarget{}, errors.New("boom")
	})
	if report.Results[0].Status != Fail || report.Results[0].Detail != "boom" {
		t.Errorf("an error should fail the case with its text: %+v", report.Results[0])
	}
}
