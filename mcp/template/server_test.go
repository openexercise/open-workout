package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// The published profile and schema, next to this module in open-workout. A copy of the template
// elsewhere skips the checks that need them.
const (
	publishedProfile  = "../tools.json"
	workoutSchemaPath = "../../schema/v0/workout.json"
)

type harness struct {
	t      *testing.T
	ctx    context.Context
	client *mcp.ClientSession
	store  *Store
	schema *jsonschema.Schema
	codes  map[string]string // error code → profile
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	catalogue, err := LoadCatalogue()
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(catalogue, "kg", time.UTC)
	now := time.Date(2026, 11, 5, 18, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	srv, err := NewServer(store)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	serverSide, clientSide := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, serverSide, nil); err != nil {
		t.Fatal(err)
	}
	client, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil).Connect(ctx, clientSide, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })

	h := &harness{t: t, ctx: ctx, client: client, store: store, codes: map[string]string{}}
	var p struct {
		ErrorCodes []struct{ Code, Profile string } `json:"error_codes"`
	}
	if err := json.Unmarshal(profileJSON, &p); err != nil {
		t.Fatal(err)
	}
	for _, c := range p.ErrorCodes {
		h.codes[c.Code] = c.Profile
	}
	if _, err := os.Stat(workoutSchemaPath); err == nil {
		if h.schema, err = jsonschema.NewCompiler().Compile(workoutSchemaPath); err != nil {
			t.Fatal(err)
		}
	}
	return h
}

// call runs a tool that should succeed and returns its structured result, checking the shape
// the profile asks for: the summary, then the same value as JSON. Every workout in it must be
// valid OWS.
func (h *harness) call(tool string, args map[string]any) map[string]any {
	h.t.Helper()
	res, err := h.client.CallTool(h.ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		h.t.Fatalf("%s: %v", tool, err)
	}
	if res.IsError {
		h.t.Fatalf("%s: %s", tool, res.Content[0].(*mcp.TextContent).Text)
	}
	if len(res.Content) != 2 {
		h.t.Fatalf("%s: %d content blocks, want the summary and the JSON", tool, len(res.Content))
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res.Content[1].(*mcp.TextContent).Text), &out); err != nil {
		h.t.Fatalf("%s: the second block isn't JSON: %v", tool, err)
	}
	if res.Content[0].(*mcp.TextContent).Text == "" {
		h.t.Fatalf("%s: no summary", tool)
	}
	if w, ok := out["workout"]; ok {
		h.valid(w)
	}
	if list, ok := out["workouts"].([]any); ok {
		for _, w := range list {
			h.valid(w)
		}
	}
	return out
}

// fails runs a tool that should fail with a code the profile lists for the core.
func (h *harness) fails(tool string, args map[string]any, code string) string {
	h.t.Helper()
	res, err := h.client.CallTool(h.ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		h.t.Fatalf("%s: %v", tool, err)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if !res.IsError || !strings.HasPrefix(text, code+": ") {
		h.t.Fatalf("%s: got %q, want a %s tool error", tool, text, code)
	}
	if h.codes[code] != "core" {
		h.t.Fatalf("%s isn't a core error code in the profile", code)
	}
	return text
}

func (h *harness) valid(w any) {
	h.t.Helper()
	if h.schema == nil {
		return
	}
	raw, _ := json.Marshal(w)
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		h.t.Fatal(err)
	}
	if err := h.schema.Validate(doc); err != nil {
		h.t.Fatalf("not a valid OWS workout: %v\n%s", err, raw)
	}
}

func TestCopyOfTheProfileIsCurrent(t *testing.T) {
	published, err := os.ReadFile(publishedProfile)
	if err != nil {
		t.Skip("no published profile next to this copy")
	}
	if !bytes.Equal(published, profileJSON) {
		t.Fatal("tools.json differs from ../tools.json: copy it over (the publish script writes both)")
	}
}

// TestToolsAreTheProfilesCore: a client sees exactly the profile's core tools and instructions.
func TestToolsAreTheProfilesCore(t *testing.T) {
	h := newHarness(t)
	listed, err := h.client.ListTools(h.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	instructions, core, err := coreProfile()
	if err != nil {
		t.Fatal(err)
	}
	if len(core) == 0 {
		t.Fatal("the profile has no core tools")
	}
	byName := map[string]*mcp.Tool{}
	for _, tool := range listed.Tools {
		byName[tool.Name] = tool
	}
	for _, want := range core {
		got, ok := byName[want.Name]
		if !ok {
			t.Errorf("%s isn't listed", want.Name)
			continue
		}
		g, _ := json.Marshal(got)
		w, _ := json.Marshal(want)
		if !bytes.Equal(g, w) {
			t.Errorf("%s differs from the profile:\n got %s\nwant %s", want.Name, g, w)
		}
	}
	if len(listed.Tools) != len(core) {
		t.Errorf("%d tools listed, want the profile's %d core tools", len(listed.Tools), len(core))
	}
	if got := h.client.InitializeResult().Instructions; got != instructions {
		t.Errorf("instructions differ from the profile's")
	}
}

func TestLogEditAndUndo(t *testing.T) {
	h := newHarness(t)

	logged := h.call("log_workout", map[string]any{
		"date": "2026-11-03", "title": "Push", "idempotency_key": "k1",
		"exercises": []any{
			map[string]any{"name": "bench press", "sets": []any{
				map[string]any{"reps": 5, "load": 100, "unit": "kg"},
				map[string]any{"reps": 5, "load": 100},
			}},
			map[string]any{"name": "db bench", "sets": []any{map[string]any{"reps": 10, "load": 30, "unit": "kg", "estimated": true}}},
			map[string]any{"name": "flurbo lift", "sets": []any{map[string]any{"reps": 8}}},
		},
	})
	w := logged["workout"].(map[string]any)
	workoutID := w["id"].(string)
	entries := w["exercises"].([]any)
	if got := entries[0].(map[string]any)["exercise"].(map[string]any)["id"]; got != "oec:barbell-bench-press" {
		t.Fatalf("bench press matched %v", got)
	}
	if got := entries[1].(map[string]any)["exercise"].(map[string]any)["match"]; got != "close" {
		t.Fatalf("db bench matched as %v, want close", got)
	}
	if got := entries[2].(map[string]any)["exercise"].(map[string]any)["match"]; got != "unmatched" {
		t.Fatalf("flurbo lift matched as %v, want unmatched", got)
	}
	confirm := logged["needs_confirmation"].([]any)
	if len(confirm) != 2 || len(confirm[0].(map[string]any)["candidates"].([]any)) == 0 {
		t.Fatalf("needs_confirmation = %v, want the close match with candidates and the unmatched one", confirm)
	}
	estimates := w["estimates"].([]any)
	if len(estimates) != 2 || estimates[0] != "/exercises/0/sets/1/load/unit" || estimates[1] != "/exercises/1/sets/0" {
		t.Fatalf("estimates = %v, want the assumed unit and the estimated set", estimates)
	}

	// A retry replays; the same key for something else is refused.
	again := h.call("log_workout", map[string]any{
		"idempotency_key": "k1", "title": "Push", "date": "2026-11-03",
		"exercises": []any{
			map[string]any{"name": "bench press", "sets": []any{
				map[string]any{"reps": 5, "load": 100, "unit": "kg"},
				map[string]any{"reps": 5, "load": 100},
			}},
			map[string]any{"name": "db bench", "sets": []any{map[string]any{"reps": 10, "load": 30, "unit": "kg", "estimated": true}}},
			map[string]any{"name": "flurbo lift", "sets": []any{map[string]any{"reps": 8}}},
		},
	})
	if again["replayed"] != true || again["workout"].(map[string]any)["id"] != workoutID {
		t.Fatalf("the retry didn't replay: %v", again["workout"].(map[string]any)["id"])
	}
	h.fails("log_workout", map[string]any{"idempotency_key": "k1", "exercises": []any{
		map[string]any{"name": "squat", "sets": []any{map[string]any{"reps": 5, "load": 140}}},
	}}, "idempotency_conflict")

	// Confirming the close match makes it count.
	closeID := confirm[0].(map[string]any)["entry_id"].(string)
	updated := h.call("update_entry", map[string]any{"entry_id": closeID, "exercise_id": "oec:dumbbell-bench-press"})
	if got := updated["workout"].(map[string]any)["exercises"].([]any)[1].(map[string]any)["exercise"].(map[string]any)["match"]; got != "catalogue" {
		t.Fatalf("after confirming, match = %v", got)
	}
	h.fails("update_entry", map[string]any{"entry_id": closeID, "reps": 3}, "invalid_input")

	// Today's session: sets join the same entry, and a heavier set is a new record.
	h.call("add_sets", map[string]any{"exercise": "squat", "sets": []any{map[string]any{"reps": 5, "load": 140, "unit": "kg"}}})
	added := h.call("add_sets", map[string]any{"exercise": "bench", "sets": []any{map[string]any{"reps": 3, "load": 110, "unit": "kg"}}})
	today := added["workout"].(map[string]any)
	if n := len(today["exercises"].([]any)); n != 2 {
		t.Fatalf("today's session has %d entries, want squat and bench", n)
	}
	added = h.call("add_sets", map[string]any{"workout_id": "current", "exercise_id": "oec:barbell-bench-press", "sets": []any{map[string]any{"reps": 2, "load": 115, "unit": "kg"}}})
	bench := added["workout"].(map[string]any)["exercises"].([]any)[1].(map[string]any)
	if n := len(bench["sets"].([]any)); n != 2 {
		t.Fatalf("bench has %d sets today, want 2 in one entry", n)
	}
	summary := added["summary"].(string)
	if !strings.Contains(summary, "New PR: Barbell Bench Press heaviest load 115 kg (was 110)") {
		t.Fatalf("summary %q doesn't announce the record", summary)
	}

	// Reads.
	list := h.call("get_workouts", map[string]any{"from": "2026-11-01"})
	if got := list["totals"].(map[string]any)["workouts"]; got != 2.0 {
		t.Fatalf("get_workouts found %v workouts", got)
	}
	onlyBench := h.call("get_workouts", map[string]any{"exercise": "oec:barbell-bench-press"})
	for _, w := range onlyBench["workouts"].([]any) {
		for _, e := range w.(map[string]any)["exercises"].([]any) {
			if id := e.(map[string]any)["exercise"].(map[string]any)["id"]; id != "oec:barbell-bench-press" {
				t.Fatalf("an exercise filter returned %v", id)
			}
		}
	}
	records := h.call("get_personal_records", map[string]any{"exercise": "bench press"})["records"].([]any)
	if len(records) != 3 {
		t.Fatalf("%d bench records, want heaviest load, e1RM and set volume", len(records))
	}
	progress := h.call("get_progress", map[string]any{"exercise": "Bench Press", "metric": "e1rm"})
	if points := progress["points"].([]any); len(points) != 2 || points[0].(map[string]any)["value"] != 116.7 {
		t.Fatalf("e1rm points = %v", progress["points"])
	}
	search := h.call("search_exercises", map[string]any{"query": "rdl"})["exercises"].([]any)
	if len(search) == 0 || search[0].(map[string]any)["id"] != "oec:barbell-romanian-deadlift" {
		t.Fatalf("search for rdl = %v", search)
	}
	h.fails("get_progress", map[string]any{"exercise": "press", "metric": "e1rm"}, "exercise_ambiguous")
	h.fails("get_progress", map[string]any{"exercise": "bench", "metric": "speed"}, "invalid_input")

	// Delete a set and undo it: it comes back with its ID, once.
	setID := bench["sets"].([]any)[1].(map[string]any)["id"].(string)
	deleted := h.call("delete_entry", map[string]any{"entry_id": setID})
	if deleted["records_changed"].([]any) == nil {
		t.Fatal("no records_changed")
	}
	token := deleted["undo_token"].(string)
	h.fails("update_entry", map[string]any{"entry_id": setID, "reps": 1}, "not_found")
	restored := h.call("undo", map[string]any{"undo_token": token})
	sets := restored["workout"].(map[string]any)["exercises"].([]any)[1].(map[string]any)["sets"].([]any)
	if sets[1].(map[string]any)["id"] != setID {
		t.Fatalf("undo restored %v, want set %s", sets[1], setID)
	}
	h.fails("undo", map[string]any{"undo_token": token}, "undo_unavailable")

	// Undo expires after 10 minutes.
	deleted = h.call("delete_entry", map[string]any{"entry_id": workoutID})
	later := h.store.now().Add(11 * time.Minute)
	h.store.now = func() time.Time { return later }
	h.fails("undo", map[string]any{"undo_token": deleted["undo_token"]}, "undo_unavailable")
}

func TestRefusals(t *testing.T) {
	h := newHarness(t)
	h.fails("log_workout", map[string]any{"exercises": []any{}}, "invalid_input")
	h.fails("log_workout", map[string]any{"exercises": []any{map[string]any{"name": "squat", "sets": []any{map[string]any{"reps": 5}}}}, "colour": "red"}, "invalid_input")
	h.fails("log_workout", map[string]any{"date": "2026-11-06", "exercises": []any{map[string]any{"name": "squat", "sets": []any{map[string]any{"reps": 5}}}}}, "invalid_input")
	h.fails("log_workout", map[string]any{"program": map[string]any{"id": "p"}, "exercises": []any{map[string]any{"name": "squat", "sets": []any{map[string]any{"reps": 5}}}}}, "invalid_input")
	h.fails("add_sets", map[string]any{"sets": []any{map[string]any{"reps": 5, "rpe": 11}}, "exercise": "squat"}, "invalid_input")
	h.fails("add_sets", map[string]any{"entry_id": "en_nope", "sets": []any{map[string]any{"reps": 5}}}, "not_found")
	h.fails("delete_entry", map[string]any{"entry_id": "wk_nope"}, "not_found")
	h.fails("search_exercises", map[string]any{"query": " "}, "invalid_input")
}

// TestPeopleAreKeptApart: one person's IDs are not_found for another.
func TestPeopleAreKeptApart(t *testing.T) {
	catalogue, err := LoadCatalogue()
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore(catalogue, "kg", time.UTC)
	res, _, err := s.Write("ana", "", "x", func(l *personLog) (Result, error) {
		return l.LogWorkout(LogWorkoutInput{Exercises: []ExerciseInput{{Name: "squat", Sets: []SetInput{{Reps: ptr(5)}}}}}, "mcp", "")
	})
	if err != nil {
		t.Fatal(err)
	}
	id := res.(Logged).Workout.ID
	_, _, err = s.Write("ben", "", "y", func(l *personLog) (Result, error) { return l.Delete(id) })
	if te, ok := err.(*ToolError); !ok || te.Code != "not_found" {
		t.Fatalf("another person deleting the workout: %v", err)
	}
}

func ptr[T any](v T) *T { return &v }
