package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestRESTPagesTheChangeFeed: the change feed comes a page at a time, oldest change first, and
// following next_cursor reaches every changed workout once. The rest of the API core is checked
// by ows conformance (TestTemplatePasses in ../../conformance).
func TestRESTPagesTheChangeFeed(t *testing.T) {
	catalogue, err := LoadCatalogue()
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(catalogue, "kg", time.UTC)
	srv, err := NewServer(store)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(newHandler(srv, &rest{store: store, hooks: NewHooks(false)}, tokenSet{write: "w", read: "r"}, "http://template.test"))
	defer ts.Close()
	do := func(method, path, token, body string) (*http.Response, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		return res, out
	}

	start := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	var ids []string
	for i := range 5 {
		res, out := do("POST", "/v1/workouts", "w", `{"title":"w`+string(rune('0'+i))+`","exercises":[{"name":"squat","sets":[{"reps":5}]}]}`)
		if res.StatusCode != http.StatusCreated {
			t.Fatalf("create: %d %v", res.StatusCode, out)
		}
		ids = append(ids, out["workout"].(map[string]any)["id"].(string))
	}
	if res, _ := do("POST", "/v1/workouts", "r", `{"exercises":[{"name":"squat","sets":[{"reps":5}]}]}`); res.StatusCode != http.StatusForbidden {
		t.Errorf("a read-only credential wrote: %d", res.StatusCode)
	}

	var seen []string
	cursor := ""
	for pages := 0; ; pages++ {
		if pages > 5 {
			t.Fatal("paging doesn't end")
		}
		q := url.Values{"updated_since": {start}, "limit": {"2"}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		res, out := do("GET", "/v1/workouts?"+q.Encode(), "r", "")
		if res.StatusCode != http.StatusOK {
			t.Fatalf("feed: %d %v", res.StatusCode, out)
		}
		for _, w := range out["workouts"].([]any) {
			seen = append(seen, w.(map[string]any)["id"].(string))
		}
		next, _ := out["next_cursor"].(string)
		if out["truncated"] != true || next == "" {
			break
		}
		cursor = next
	}
	// Ties on updated_at go by ID, so compare as sets; each must come exactly once.
	slices.Sort(seen)
	slices.Sort(ids)
	if !slices.Equal(seen, ids) {
		t.Errorf("paged %v, want each of %v once", seen, ids)
	}
	if res, _ := do("GET", "/v1/workouts?updated_since="+url.QueryEscape(start)+"&cursor=nonsense", "r", ""); res.StatusCode != http.StatusBadRequest {
		t.Errorf("a bad cursor: %d", res.StatusCode)
	}
}

// TestRESTReadsAWorkoutDocument: an OWS document posted as it is keeps its estimates, read as
// pointers into the body, and refuses a pointer that points at nothing.
func TestRESTReadsAWorkoutDocument(t *testing.T) {
	catalogue, err := LoadCatalogue()
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(catalogue, "kg", time.UTC)
	srv, err := NewServer(store)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(newHandler(srv, &rest{store: store, hooks: NewHooks(false)}, tokenSet{write: "w", read: "r"}, "http://template.test"))
	defer ts.Close()
	post := func(body string) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest("POST", ts.URL+"/v1/workouts", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer w")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}
	doc := `{"$schema": "https://openworkout.dev/schema/v0/workout.json", "date": "2026-11-01", "timezone": "Europe/Berlin",
		"exercises": [{"exercise": {"id": "oec:barbell-back-squat", "name": "Barbell Back Squat", "match": "catalogue"},
			"sets": [{"reps": 5, "load": {"value": 100, "unit": "kg"}}, {"reps": 5, "load": 100, "x-app": {"n": 1}}]}],
		"estimates": ["/exercises/0/sets/0/reps", "/exercises/0/sets/1/load/unit"], "x-app": true}`
	status, out := post(doc)
	if status != http.StatusCreated {
		t.Fatalf("post: %d %v", status, out)
	}
	w := out["workout"].(map[string]any)
	got := fmt.Sprint(w["estimates"])
	if want := "[/started_at /exercises/0/sets/0 /exercises/0/sets/1/load/unit]"; got != want {
		t.Errorf("estimates = %s, want %s", got, want)
	}
	if w["date"] != "2026-11-01" || w["timezone"] != "Europe/Berlin" {
		t.Errorf("date %v, timezone %v; want the day and zone sent", w["date"], w["timezone"])
	}
	for _, bad := range []string{`"/exercises/0/sets/2"`, `"/exercises/0"`, `"/title"`} {
		body := strings.Replace(doc, `"/exercises/0/sets/0/reps"`, bad, 1)
		if status, out := post(body); status != http.StatusBadRequest || out["field"] != "estimates[0]" {
			t.Errorf("estimates %s: %d %v, want invalid_input on estimates[0]", bad, status, out)
		}
	}
}
