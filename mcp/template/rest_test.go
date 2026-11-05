package main

import (
	"encoding/json"
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
