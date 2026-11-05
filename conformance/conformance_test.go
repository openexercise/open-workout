package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// reference is an in-memory server keeping to the reference API as far as the script goes. A
// broken field turns one behaviour off, so a test can check the right check catches it.
type reference struct {
	mu        sync.Mutex
	next      int
	byID      map[string]map[string]any
	keys      map[string]string
	external  map[string]string
	deleted   map[string]bool
	deletedAt map[string]string
	hooks     map[string]bool

	noChallenge, noReplay, noExternalReplay, revives, loseNotes, badExport bool
	noFeed, noTombstones, listsSecret                                      bool
}

func newReference() *reference {
	return &reference{byID: map[string]map[string]any{}, keys: map[string]string{}, external: map[string]string{}, deleted: map[string]bool{},
		deletedAt: map[string]string{}, hooks: map[string]bool{}}
}

func (s *reference) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer good" {
		if !s.noChallenge {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="http://example.test/.well-known/oauth-protected-resource"`)
		}
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/workouts":
		s.create(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/workouts":
		s.feed(w)
	case r.URL.Path == "/v1/webhooks" || strings.HasPrefix(r.URL.Path, "/v1/webhooks/"):
		s.webhooks(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/export":
		s.export(w)
	case strings.HasPrefix(r.URL.Path, "/v1/workouts/"):
		id := strings.TrimPrefix(r.URL.Path, "/v1/workouts/")
		wk, ok := s.byID[id]
		if !ok || s.deleted[id] {
			problem(w, http.StatusNotFound, "not_found")
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, wk)
		case http.MethodPatch:
			var patch map[string]any
			_ = json.NewDecoder(r.Body).Decode(&patch)
			for k, v := range patch {
				if k == "notes" && s.loseNotes {
					continue
				}
				wk[k] = v
			}
			wk["updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)
			writeJSON(w, http.StatusOK, map[string]any{"workout": wk})
		case http.MethodDelete:
			s.deleted[id] = true
			s.deletedAt[id] = time.Now().UTC().Format(time.RFC3339Nano)
			writeJSON(w, http.StatusOK, map[string]any{"undo_token": "u"})
		}
	default:
		problem(w, http.StatusNotFound, "not_found")
	}
}

func (s *reference) create(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	_ = json.NewDecoder(r.Body).Decode(&in)
	key := r.Header.Get("Idempotency-Key")
	ext, _ := in["external_id"].(string)
	_, seenKey := s.keys[key]
	if id, ok := s.keys[key]; ok && !s.noReplay {
		s.replay(w, id)
		return
	}
	// Without key replay, a repeated key stores a duplicate whatever the external ID says.
	if id, ok := s.external[ext]; ok && ext != "" && !s.noExternalReplay && !(seenKey && s.noReplay) {
		if s.deleted[id] && !s.revives {
			problem(w, http.StatusNotFound, "not_found")
			return
		}
		delete(s.deleted, id)
		s.replay(w, id)
		return
	}
	s.next++
	id := fmt.Sprintf("wk_%d", s.next)
	var entries []any
	for _, e := range in["exercises"].([]any) {
		e := e.(map[string]any)
		var sets []any
		for _, st := range e["sets"].([]any) {
			st := st.(map[string]any)
			set := map[string]any{"reps": st["reps"]}
			if st["load"] != nil {
				set["load"] = map[string]any{"value": st["load"], "unit": st["unit"]}
			}
			sets = append(sets, set)
		}
		entries = append(entries, map[string]any{
			"exercise": map[string]any{"id": nil, "name": e["name"], "match": "unmatched"},
			"text":     e["name"], "sets": sets,
		})
	}
	wk := map[string]any{
		"$schema": "https://openworkout.dev/schema/v0/workout.json",
		"id":      id, "started_at": in["started_at"], "title": in["title"], "notes": in["notes"],
		"updated_at": time.Now().UTC().Format(time.RFC3339Nano),
		"source":     map[string]any{"kind": "api", "client": "test", "external_id": ext},
		"exercises":  entries, "estimates": []any{},
	}
	s.byID[id], s.keys[key] = wk, id
	if _, ok := s.external[ext]; !ok {
		s.external[ext] = id
	}
	w.Header().Set("Location", "/v1/workouts/"+id)
	writeJSON(w, http.StatusCreated, map[string]any{"workout": wk, "summary": "Logged."})
}

func (s *reference) replay(w http.ResponseWriter, id string) {
	w.Header().Set("Idempotent-Replayed", "true")
	w.Header().Set("Location", "/v1/workouts/"+id)
	writeJSON(w, http.StatusOK, map[string]any{"workout": s.byID[id], "replayed": true})
}

func (s *reference) export(w http.ResponseWriter) {
	var out []any
	for id, wk := range s.byID {
		if !s.deleted[id] {
			out = append(out, wk)
		}
	}
	if s.badExport {
		out = append(out, map[string]any{"id": "broken"})
	}
	writeJSON(w, http.StatusOK, map[string]any{"exported_at": time.Now().UTC(), "workouts": out, "routines": []any{}})
}

// feed is the change feed: every stored workout, and the deleted ones as tombstones.
func (s *reference) feed(w http.ResponseWriter) {
	workouts, deleted := []any{}, []any{}
	for id, wk := range s.byID {
		switch {
		case s.noFeed:
		case !s.deleted[id]:
			workouts = append(workouts, wk)
		case !s.noTombstones:
			deleted = append(deleted, map[string]any{"id": id, "deleted_at": s.deletedAt[id], "updated_at": s.deletedAt[id]})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"workouts": workouts, "deleted": deleted})
}

func (s *reference) webhooks(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost:
		s.next++
		id := fmt.Sprintf("wh_%d", s.next)
		s.hooks[id] = true
		writeJSON(w, http.StatusCreated, map[string]any{"id": id, "secret": "whsec"})
	case r.Method == http.MethodGet:
		var list []any
		for id := range s.hooks {
			h := map[string]any{"id": id}
			if s.listsSecret {
				h["secret"] = "whsec"
			}
			list = append(list, h)
		}
		writeJSON(w, http.StatusOK, map[string]any{"webhooks": list})
	case r.Method == http.MethodDelete:
		delete(s.hooks, strings.TrimPrefix(r.URL.Path, "/v1/webhooks/"))
		w.WriteHeader(http.StatusNoContent)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func problem(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "code": code})
}

func run(t *testing.T, s *reference) Report {
	t.Helper()
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	report, err := Run(context.Background(), Config{BaseURL: srv.URL, Token: "good", HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func statuses(r Report) map[string]Status {
	out := map[string]Status{}
	for _, res := range r.Results {
		out[res.Name] = res.Status
	}
	return out
}

func TestReferenceServerPasses(t *testing.T) {
	s := newReference()
	report := run(t, s)
	if !report.Passed() {
		for _, res := range report.Results {
			t.Logf("%s %s %s", res.Status, res.Name, res.Detail)
		}
		t.Fatal("the reference server should pass")
	}
	// The run cleans up after itself: its one workout is deleted, and its webhook removed.
	for id := range s.byID {
		if !s.deleted[id] {
			t.Errorf("%s left stored", id)
		}
	}
	if len(s.hooks) > 0 {
		t.Errorf("webhooks left subscribed: %v", s.hooks)
	}
}

// Each broken behaviour fails the check that covers it, and only checks that depend on it skip.
func TestEachCheckCatchesItsBehaviour(t *testing.T) {
	cases := []struct {
		name   string
		breaks func(*reference)
		fails  []string
	}{
		{"no challenge", func(s *reference) { s.noChallenge = true }, []string{"discovery"}},
		{"no replay", func(s *reference) { s.noReplay = true }, []string{"replay"}},
		// Without external IDs, a deleted workout's ID can't stay deleted either.
		{"no external replay", func(s *reference) { s.noExternalReplay = true }, []string{"external-id", "stays-deleted"}},
		{"notes not changed", func(s *reference) { s.loseNotes = true }, []string{"update"}},
		{"invalid export", func(s *reference) { s.badExport = true }, []string{"export"}},
		{"deleted comes back", func(s *reference) { s.revives = true }, []string{"stays-deleted"}},
		{"no change feed", func(s *reference) { s.noFeed = true }, []string{"change-feed", "tombstone"}},
		{"no tombstones", func(s *reference) { s.noTombstones = true }, []string{"tombstone"}},
		{"secret listed", func(s *reference) { s.listsSecret = true }, []string{"webhooks"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newReference()
			c.breaks(s)
			got := statuses(run(t, s))
			for _, name := range Checks {
				want := Pass
				for _, f := range c.fails {
					if name == f {
						want = Fail
					}
				}
				if got[name] != want {
					t.Errorf("%s = %s, want %s", name, got[name], want)
				}
			}
		})
	}
}

// A server that can't store the workout fails create, and every check that needs it skips,
// rather than piling up failures that all say the same thing.
func TestCreateFailureSkipsTheRest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="x"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		problem(w, http.StatusForbidden, "consent_required")
	}))
	defer srv.Close()
	report, err := Run(context.Background(), Config{BaseURL: srv.URL, Token: "t", HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	got := statuses(report)
	if got["discovery"] != Pass || got["create"] != Fail {
		t.Fatalf("got %v", got)
	}
	if !strings.Contains(report.Results[1].Detail, "consent_required") {
		t.Errorf("create detail %q should name the problem code", report.Results[1].Detail)
	}
	for _, name := range Checks[2:] {
		want := Skip
		if name == "webhooks" { // needs no workout, so it runs, and fails here like everything else
			want = Fail
		}
		if got[name] != want {
			t.Errorf("%s = %s, want %s", name, got[name], want)
		}
	}
	if report.Passed() {
		t.Error("a report with failures passed")
	}
}

func TestConfigIsChecked(t *testing.T) {
	for _, cfg := range []Config{{BaseURL: "ftp://x", Token: "t"}, {BaseURL: "https://x"}, {BaseURL: "not a url", Token: "t"}} {
		if _, err := Run(context.Background(), cfg); err == nil {
			t.Errorf("%+v: no error", cfg)
		}
	}
}
