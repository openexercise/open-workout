package openworkout

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeAPI records every request and answers with handle.
type fakeAPI struct {
	t        *testing.T
	requests []*http.Request
	bodies   []string
	handle   func(w http.ResponseWriter, r *http.Request)
}

func newFake(t *testing.T, handle func(w http.ResponseWriter, r *http.Request)) (*Client, *fakeAPI) {
	t.Helper()
	f := &fakeAPI{t: t, handle: handle}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.requests = append(f.requests, r)
		f.bodies = append(f.bodies, string(body))
		f.handle(w, r)
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL+"/", "lca_test")
	c.NewIdempotencyKey = func() string { return "key-1" }
	return c, f
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func writeProblem(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "https://lifterclub.com/docs/api#" + code, "title": code, "status": status, "code": code, "detail": detail})
}

func TestRequestsCarryTheTokenAndAnIdempotencyKeyOnPOST(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusCreated, `{"workout": {"id": "w1", "started_at": "2026-11-03T10:00:00Z", "source": {"kind": "api", "client": "test"}, "exercises": []}, "summary": "Logged", "new_records": [], "needs_confirmation": []}`)
	})
	c.UserAgent = "test-app/1.0"
	reps := 5
	logged, err := c.LogWorkout(context.Background(), WorkoutInput{ExternalID: "my-1", Exercises: []ExerciseInput{{Name: "bench press", Sets: []SetInput{{Reps: &reps}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if logged.Workout.ID != "w1" || logged.Summary != "Logged" {
		t.Errorf("logged = %+v", logged)
	}
	r := f.requests[0]
	if r.Method != http.MethodPost || r.URL.Path != "/v1/workouts" {
		t.Errorf("%s %s", r.Method, r.URL)
	}
	for header, want := range map[string]string{"Authorization": "Bearer lca_test", "Idempotency-Key": "key-1", "Content-Type": "application/json", "User-Agent": "test-app/1.0"} {
		if got := r.Header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if !strings.Contains(f.bodies[0], `"external_id":"my-1"`) || strings.Contains(f.bodies[0], `"started_at"`) {
		t.Errorf("body = %s", f.bodies[0])
	}

	// A GET carries no key; a key from the context wins over a fresh one.
	if _, err := c.Me(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.requests[1].Header["Idempotency-Key"]; ok {
		t.Error("a GET sent an Idempotency-Key")
	}
	_, _ = c.LogText(WithIdempotencyKey(context.Background(), "mine"), TextRequest{Text: "bench 3x5"})
	if got := f.requests[2].Header.Get("Idempotency-Key"); got != "mine" {
		t.Errorf("key from context = %q", got)
	}
}

func TestTokenSourceIsAskedPerRequest(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, `{}`) })
	n := 0
	c.TokenSource = func(context.Context) (string, error) { n++; return fmt.Sprintf("lca_%d", n), nil }
	for range 2 {
		if _, err := c.Me(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if a, b := f.requests[0].Header.Get("Authorization"), f.requests[1].Header.Get("Authorization"); a != "Bearer lca_1" || b != "Bearer lca_2" {
		t.Errorf("tokens %q, %q", a, b)
	}
	c.TokenSource = func(context.Context) (string, error) { return "", errors.New("expired") }
	if _, err := c.Me(context.Background()); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Errorf("token error = %v", err)
	}
}

func TestProblemsBecomeErrors(t *testing.T) {
	c, _ := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/workouts":
			w.Header().Set("Retry-After", "60")
			writeProblem(w, http.StatusTooManyRequests, "rate_limited", "at most 60 writes a minute per account, shared with MCP")
		case "/v1/workouts/missing":
			writeProblem(w, http.StatusNotFound, "not_found", "no such resource in this account")
		case "/v1/me":
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="https://api.test/.well-known/oauth-protected-resource", error="invalid_token"`)
			writeJSON(w, http.StatusUnauthorized, `{"error": "invalid_token", "error_description": "expired"}`)
		case "/v1/profile":
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, "<html>bad gateway</html>")
		default:
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(map[string]any{"type": "x", "title": "x", "status": 422, "code": "exercise_ambiguous", "detail": "retry with one of the candidates' IDs",
				"candidates": []map[string]any{{"id": "oec:a", "name": "A", "measurement_type": "weight_reps", "is_custom": false, "score": 0.9}}})
		}
	})
	ctx := context.Background()

	_, err := c.LogWorkout(ctx, WorkoutInput{})
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeRateLimited || e.Status != 429 || e.RetryAfter != time.Minute || !strings.Contains(e.Detail, "per account") {
		t.Fatalf("rate limited: %#v", err)
	}
	if !HasCode(err, CodeRateLimited) || HasCode(err, CodeNotFound) {
		t.Error("HasCode")
	}
	if want := "openworkout: rate_limited (429): at most 60 writes a minute per account, shared with MCP"; err.Error() != want {
		t.Errorf("Error() = %q", err)
	}

	if _, err := c.GetWorkout(ctx, "missing"); !HasCode(err, CodeNotFound) {
		t.Errorf("not found: %v", err)
	}

	_, err = c.Me(ctx)
	if !errors.As(err, &e) || e.Code != CodeInvalidToken || e.Status != 401 || e.Detail != "expired" || !strings.Contains(e.WWWAuthenticate, "resource_metadata") {
		t.Errorf("bearer error: %#v", err)
	}

	_, err = c.Profile(ctx)
	if !errors.As(err, &e) || e.Code != "http_502" || e.Status != 502 || e.RetryAfter != 0 {
		t.Errorf("non-JSON failure: %#v", err)
	}

	_, err = c.Records(ctx, "row")
	if !errors.As(err, &e) || e.Code != CodeExerciseAmbiguous || len(e.Candidates) != 1 || e.Candidates[0].ID != "oec:a" {
		t.Errorf("ambiguous: %#v", err)
	}
}

func TestWorkoutsFollowsTheCursorWithTheSameFilters(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "" {
			writeJSON(w, 200, `{"workouts": [{"id": "w1", "started_at": "2026-11-03T10:00:00Z", "source": {"kind": "app"}, "exercises": []}, {"id": "w2", "started_at": "2026-11-02T10:00:00Z", "source": {"kind": "app"}, "exercises": []}], "totals": {"workouts": 2, "sets": 0, "volume": 0, "unit": "lb"}, "truncated": true, "next_cursor": "c2", "summary": ""}`)
			return
		}
		writeJSON(w, 200, `{"workouts": [{"id": "w3", "started_at": "2026-11-01T10:00:00Z", "source": {"kind": "app"}, "exercises": []}], "totals": {"workouts": 1, "sets": 0, "volume": 0, "unit": "lb"}, "truncated": false, "summary": ""}`)
	})
	var ids []string
	for w, err := range c.Workouts(context.Background(), ListWorkoutsOptions{From: "2026-10-01", Exercise: "bench", Limit: 2}) {
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, w.ID)
	}
	if got := strings.Join(ids, ","); got != "w1,w2,w3" {
		t.Errorf("ids = %s", got)
	}
	if q := f.requests[0].URL.RawQuery; q != "exercise=bench&from=2026-10-01&limit=2" {
		t.Errorf("first page query = %s", q)
	}
	if q := f.requests[1].URL.RawQuery; q != "cursor=c2&exercise=bench&from=2026-10-01&limit=2" {
		t.Errorf("second page query = %s", q)
	}

	// Stopping early stops asking.
	f.requests = nil
	for range c.Workouts(context.Background(), ListWorkoutsOptions{}) {
		break
	}
	if len(f.requests) != 1 {
		t.Errorf("%d requests after break", len(f.requests))
	}
}

func TestChangesYieldsWorkoutsThenTombstonesAndEndsOnAnError(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("cursor") {
		case "":
			writeJSON(w, 200, `{"workouts": [{"id": "w1", "started_at": "2026-11-03T10:00:00Z", "updated_at": "2026-11-03T10:05:00Z", "source": {"kind": "api"}, "exercises": []}], "deleted": [{"id": "d1", "deleted_at": "2026-11-03T11:00:00Z", "updated_at": "2026-11-03T11:00:00Z", "merged_into": "w9"}], "updated_since": "2026-11-01T00:00:00Z", "totals": {"workouts": 1, "sets": 0, "volume": 0, "unit": "kg"}, "truncated": true, "next_cursor": "c2", "summary": ""}`)
		case "c2":
			writeProblem(w, http.StatusTooManyRequests, "rate_limited", "the app's limit")
		}
	})
	var seen []string
	var last time.Time
	var failure error
	for ch, err := range c.Changes(context.Background(), ChangesOptions{UpdatedSince: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), IncludeDeleted: true, Limit: 50}) {
		if err != nil {
			failure = err
			break
		}
		switch {
		case ch.Workout != nil:
			seen = append(seen, "workout:"+ch.Workout.ID)
		case ch.Deleted != nil:
			seen = append(seen, "deleted:"+ch.Deleted.ID+"->"+ch.Deleted.MergedInto)
		}
		if ch.UpdatedAt().After(last) {
			last = ch.UpdatedAt()
		}
	}
	if got := strings.Join(seen, ","); got != "workout:w1,deleted:d1->w9" {
		t.Errorf("changes = %s", got)
	}
	if !last.Equal(time.Date(2026, 11, 3, 11, 0, 0, 0, time.UTC)) {
		t.Errorf("latest updated_at = %v", last)
	}
	if !HasCode(failure, CodeRateLimited) {
		t.Errorf("the failed page's error = %v", failure)
	}
	if q := f.requests[0].URL.RawQuery; q != "include_deleted=true&limit=50&updated_since=2026-11-01T00%3A00%3A00Z" {
		t.Errorf("query = %s", q)
	}
}

func TestPathsAndMethods(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v1/webhooks/") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, 200, `{"exercises": [{"id": "oec:bench-press", "name": "Bench Press", "measurement_type": "weight_reps", "equipment": [], "primary_muscles": [], "is_custom": false, "score": 1}], "webhooks": [{"id": "h1", "url": "https://p.test", "events": ["workout.created"], "created_at": "2026-11-03T10:00:00Z", "consecutive_failures": 0}], "records": [], "points": []}`)
	})
	ctx := context.Background()
	notes := "felt good"
	calls := []struct {
		call         func() error
		method, path string
		query        string
	}{
		{func() error { _, err := c.ListWorkouts(ctx, ListWorkoutsOptions{To: "2026-11-03"}); return err }, "GET", "/v1/workouts", "to=2026-11-03"},
		{func() error { _, err := c.GetWorkout(ctx, "w/1"); return err }, "GET", "/v1/workouts/w%2F1", ""},
		{func() error { _, err := c.UpdateWorkout(ctx, "w1", WorkoutPatch{Notes: &notes}); return err }, "PATCH", "/v1/workouts/w1", ""},
		{func() error { _, err := c.DeleteWorkout(ctx, "w1"); return err }, "DELETE", "/v1/workouts/w1", ""},
		{func() error { _, err := c.Undo(ctx, "lcu_x"); return err }, "POST", "/v1/undo", ""},
		{func() error { _, err := c.Parse(ctx, TextRequest{Text: "x"}); return err }, "POST", "/v1/parse", ""},
		{func() error { _, err := c.AddSets(ctx, WorkoutCurrent, AddSetsRequest{Exercise: "squat"}); return err }, "POST", "/v1/workouts/current/sets", ""},
		{func() error { _, err := c.AddEntrySets(ctx, "e1", nil); return err }, "POST", "/v1/entries/e1/sets", ""},
		{func() error { _, err := c.UpdateEntry(ctx, "e1", EntryPatch{ExerciseID: "oec:x"}); return err }, "PATCH", "/v1/entries/e1", ""},
		{func() error { _, err := c.DeleteEntry(ctx, "e1"); return err }, "DELETE", "/v1/entries/e1", ""},
		{func() error { _, err := c.UpdateSet(ctx, "s1", SetPatch{RemoveLoad: true}); return err }, "PATCH", "/v1/sets/s1", ""},
		{func() error { _, err := c.DeleteSet(ctx, "s1"); return err }, "DELETE", "/v1/sets/s1", ""},
		{func() error { _, err := c.Records(ctx, ""); return err }, "GET", "/v1/records", ""},
		{func() error {
			_, err := c.Progress(ctx, ProgressOptions{Exercise: "bench", Metric: "e1rm", From: "2026-01-01"})
			return err
		}, "GET", "/v1/progress", "exercise=bench&from=2026-01-01&metric=e1rm"},
		{func() error {
			got, err := c.SearchExercises(ctx, SearchOptions{Query: "rdl", Limit: 5})
			if err == nil && (len(got) != 1 || got[0].ID != "oec:bench-press") {
				return fmt.Errorf("search = %+v", got)
			}
			return err
		}, "GET", "/v1/exercises", "limit=5&query=rdl"},
		{func() error { _, err := c.CreateExercise(ctx, ExerciseRequest{Name: "Flurbo Lift"}); return err }, "POST", "/v1/exercises", ""},
		{func() error { _, err := c.LastSets(ctx, "oec:bench-press"); return err }, "GET", "/v1/exercises/oec:bench-press/last-sets", ""},
		{func() error { _, err := c.Profile(ctx); return err }, "GET", "/v1/profile", ""},
		{func() error { _, err := c.UpdateProfile(ctx, ProfilePatch{Units: Kg}); return err }, "PATCH", "/v1/profile", ""},
		{func() error {
			_, err := c.CreateWebhook(ctx, WebhookInput{URL: "https://p.test", Events: []string{EventWorkoutCreated}})
			return err
		}, "POST", "/v1/webhooks", ""},
		{func() error {
			got, err := c.ListWebhooks(ctx)
			if err == nil && (len(got) != 1 || got[0].ID != "h1") {
				return fmt.Errorf("webhooks = %+v", got)
			}
			return err
		}, "GET", "/v1/webhooks", ""},
		{func() error { return c.DeleteWebhook(ctx, "h1") }, "DELETE", "/v1/webhooks/h1", ""},
		{func() error {
			body, err := c.Export(ctx, "csv")
			if err == nil {
				_, err = io.ReadAll(body)
				body.Close()
			}
			return err
		}, "GET", "/v1/export", "format=csv"},
		{func() error { return c.Do(ctx, http.MethodGet, "/v1/routines", nil, nil, nil) }, "GET", "/v1/routines", ""},
	}
	for i, tc := range calls {
		if err := tc.call(); err != nil {
			t.Errorf("%s %s: %v", tc.method, tc.path, err)
			continue
		}
		r := f.requests[i]
		if r.Method != tc.method || r.URL.EscapedPath() != tc.path || r.URL.RawQuery != tc.query {
			t.Errorf("call %d: %s %s?%s, want %s %s?%s", i, r.Method, r.URL.EscapedPath(), r.URL.RawQuery, tc.method, tc.path, tc.query)
		}
		if _, has := r.Header["Idempotency-Key"]; has != (tc.method == http.MethodPost) {
			t.Errorf("%s %s: Idempotency-Key sent = %v", tc.method, tc.path, has)
		}
	}
	// A patch sends only the fields set.
	if body := f.bodies[2]; body != `{"notes":"felt good"}` {
		t.Errorf("workout patch body = %s", body)
	}
	if body := f.bodies[10]; body != `{"remove_load":true}` {
		t.Errorf("set patch body = %s", body)
	}
}

func TestVerifySignature(t *testing.T) {
	secret, body := "lcw_secret", []byte(`{"event":"workout.created","workout_id":"w1"}`)
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	sign := func(at time.Time) string {
		return fmt.Sprintf("t=%d,v1=%s", at.Unix(), signHex(secret, at.Unix(), body))
	}
	if !VerifySignature(secret, sign(now), body, now, 5*time.Minute) {
		t.Error("a good signature was refused")
	}
	if !VerifySignature(secret, sign(now.Add(-4*time.Minute)), body, now, 5*time.Minute) {
		t.Error("a signature within the tolerance was refused")
	}
	for name, header := range map[string]string{
		"stale":        sign(now.Add(-6 * time.Minute)),
		"empty":        "",
		"no v1":        fmt.Sprintf("t=%d", now.Unix()),
		"bad hex":      fmt.Sprintf("t=%d,v1=zz", now.Unix()),
		"other secret": fmt.Sprintf("t=%d,v1=%s", now.Unix(), signHex("other", now.Unix(), body)),
	} {
		if VerifySignature(secret, header, body, now, 5*time.Minute) {
			t.Errorf("%s: accepted", name)
		}
	}
	if VerifySignature(secret, sign(now), append(body, ' '), now, 5*time.Minute) {
		t.Error("a changed body was accepted")
	}
}

// signHex is the server's signing rule, written out again so the test doesn't trust the code
// it checks: hex HMAC-SHA256 over "<t>.<body>".
func signHex(secret string, t int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%d.%s", t, body)
	return hex.EncodeToString(mac.Sum(nil))
}
