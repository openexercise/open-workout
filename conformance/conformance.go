// Package conformance checks that a server offers the reference REST API over an Open Workout
// Schema log, by running a fixed script against it as one person: log a workout, read it back
// equal, replay the Idempotency-Key and the external ID, change it, export the log and validate
// every workout in it, delete it, and check a deleted external ID stays deleted.
//
// The script writes one workout to the account and deletes it at the end, also when a check
// fails part way. Use a test account: the token needs workouts:read and workouts:write, for a
// person who has given whatever consent the server asks before it stores health data.
//
// "Conformant" is self-reported: a server that passes may say so. The reference implementation
// is Lifterclub (https://lifterclub.com/developers), whose CI runs this suite.
package conformance

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	openworkout "github.com/openexercise/open-workout"
	"github.com/openexercise/open-workout/internal/ows"
)

// Checks are the script's steps, in the order they run.
var Checks = []string{
	"discovery", "create", "read", "replay", "external-id", "update", "export", "delete", "stays-deleted",
}

// Status is a check's outcome.
type Status string

const (
	Pass Status = "pass"
	Fail Status = "fail"
	// Skip is a check that couldn't run because one it depends on failed.
	Skip Status = "skip"
)

// Result is one check's outcome.
type Result struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	// Detail says what was wrong, or why the check was skipped.
	Detail string `json:"detail,omitempty"`
}

// Report is every check's outcome, in order.
type Report struct {
	BaseURL string   `json:"base_url"`
	Results []Result `json:"results"`
}

// Passed reports whether every check passed.
func (r Report) Passed() bool {
	for _, res := range r.Results {
		if res.Status != Pass {
			return false
		}
	}
	return len(r.Results) == len(Checks)
}

// Config says where to run the script and as whom.
type Config struct {
	// BaseURL is the API's origin, such as https://api.lifterclub.com. Routes are under /v1.
	BaseURL string
	// Token is an OAuth access token with workouts:read and workouts:write.
	Token string
	// HTTPClient sends the requests; http.DefaultClient when nil.
	HTTPClient *http.Client
	// Now is the clock the workout's start time comes from; time.Now when nil.
	Now func() time.Time
}

// Run runs every check in order and reports each one. The error is for a run that couldn't
// start (a bad base URL, no token); a failing server is a report with failures, not an error.
func Run(ctx context.Context, cfg Config) (Report, error) {
	base, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return Report{}, fmt.Errorf("base URL %q: want an http or https origin", cfg.BaseURL)
	}
	if cfg.Token == "" {
		return Report{}, fmt.Errorf("no token: the script logs a workout, so it needs workouts:read and workouts:write")
	}
	v, err := ows.NewValidatorFS(openworkout.Schemas)
	if err != nil {
		return Report{}, err
	}
	r := &runner{cfg: cfg, base: base, client: cfg.HTTPClient, v: v, now: cfg.Now, run: random()}
	if r.client == nil {
		r.client = http.DefaultClient
	}
	if r.now == nil {
		r.now = time.Now
	}
	r.input = r.workoutInput()
	report := Report{BaseURL: base.String()}
	steps := []func(context.Context) error{
		r.discovery, r.create, r.read, r.replay, r.externalID, r.update, r.export, r.delete, r.staysDeleted,
	}
	for i, step := range steps {
		res := Result{Name: Checks[i], Status: Pass}
		if why := r.blocked(Checks[i]); why != "" {
			res.Status, res.Detail = Skip, why
		} else if err := step(ctx); err != nil {
			res.Status, res.Detail = Fail, err.Error()
		}
		report.Results = append(report.Results, res)
	}
	r.cleanUp(ctx)
	return report, nil
}

// blocked says why a check can't run, when one it depends on failed.
func (r *runner) blocked(check string) string {
	switch {
	case check == "discovery" || check == "create":
		return ""
	case r.id == "":
		return "needs a workout, which create didn't store"
	case check == "stays-deleted" && !r.deleted:
		return "needs the workout deleted, which delete didn't do"
	}
	return ""
}

type runner struct {
	cfg    Config
	base   *url.URL
	client *http.Client
	v      *ows.Validator
	now    func() time.Time
	run    string

	input   map[string]any
	key     string         // the create's Idempotency-Key
	id      string         // the stored workout's ID
	path    string         // where it lives, from Location
	stored  map[string]any // the workout as create returned it
	deleted bool
}

const (
	newTitle = "ows conformance (changed)"
	newNotes = "Changed by ows conformance."
)

// workoutInput is the workout the script logs: two exercises, loaded and bodyweight sets, a
// start an hour ago to the second, and an external ID unique to this run.
func (r *runner) workoutInput() map[string]any {
	return map[string]any{
		"started_at":  r.now().Add(-time.Hour).UTC().Truncate(time.Second).Format(time.RFC3339),
		"title":       "ows conformance " + r.run,
		"notes":       "Written by ows conformance and deleted at the end of its run.",
		"external_id": "ows-conformance-" + r.run,
		"exercises": []any{
			map[string]any{"name": "Barbell Bench Press", "sets": []any{
				map[string]any{"reps": 5, "load": 100, "unit": "kg"},
				map[string]any{"reps": 5, "load": 100, "unit": "kg"},
			}},
			map[string]any{"name": "Pull-up", "sets": []any{
				map[string]any{"reps": 8},
			}},
		},
	}
}

// discovery: without a token, the API answers 401 and points at its protected resource
// metadata (RFC 9728), which is how a client finds the authorization server.
func (r *runner) discovery(ctx context.Context) error {
	res, err := r.send(ctx, http.MethodGet, "/v1/workouts", nil, nil, false)
	if err != nil {
		return err
	}
	if res.status != http.StatusUnauthorized {
		return fmt.Errorf("GET /v1/workouts without a token: status %d, want 401", res.status)
	}
	challenge := res.header.Get("WWW-Authenticate")
	if !strings.HasPrefix(strings.ToLower(challenge), "bearer") || !strings.Contains(challenge, "resource_metadata=") {
		return fmt.Errorf("WWW-Authenticate %q: want a Bearer challenge with resource_metadata", challenge)
	}
	return nil
}

// create: POST /v1/workouts stores the workout and answers 201 with Location and the stored
// workout, a valid OWS document holding what was sent.
func (r *runner) create(ctx context.Context) error {
	r.key = "ows-conformance-" + random()
	res, err := r.send(ctx, http.MethodPost, "/v1/workouts", r.input, header("Idempotency-Key", r.key), true)
	if err != nil {
		return err
	}
	if res.status != http.StatusCreated {
		return res.unexpected("POST /v1/workouts", http.StatusCreated)
	}
	w, err := r.written(res)
	if err != nil {
		return err
	}
	id, _ := w["id"].(string)
	if id == "" {
		return fmt.Errorf("the stored workout has no id")
	}
	r.id, r.stored = id, w
	r.path = "/v1/workouts/" + url.PathEscape(id)
	if loc := res.header.Get("Location"); loc == "" {
		return fmt.Errorf("201 without a Location header")
	} else if u, err := r.base.Parse(loc); err != nil || !strings.HasSuffix(u.Path, "/"+id) {
		return fmt.Errorf("Location %q doesn't name workout %s", loc, id)
	}
	return matchesInput(w, r.input)
}

// read: GET the workout back, valid and equal to what create returned.
func (r *runner) read(ctx context.Context) error {
	w, err := r.get(ctx)
	if err != nil {
		return err
	}
	if diff := differ(stable(r.stored), stable(w)); diff != "" {
		return fmt.Errorf("read back differs from what create returned: %s", diff)
	}
	return nil
}

// replay: the same Idempotency-Key and body return the first result (200, Idempotent-Replayed)
// and store nothing new.
func (r *runner) replay(ctx context.Context) error {
	res, err := r.send(ctx, http.MethodPost, "/v1/workouts", r.input, header("Idempotency-Key", r.key), true)
	if err != nil {
		return err
	}
	return r.replayed(res, "a repeated Idempotency-Key")
}

// external-id: a new Idempotency-Key with the same external_id replays too, so an app syncing
// its own records never logs one twice.
func (r *runner) externalID(ctx context.Context) error {
	res, err := r.send(ctx, http.MethodPost, "/v1/workouts", r.input, header("Idempotency-Key", "ows-conformance-"+random()), true)
	if err != nil {
		return err
	}
	return r.replayed(res, "a repeated external_id")
}

func (r *runner) replayed(res *response, what string) error {
	if res.status != http.StatusOK {
		return res.unexpected("POST /v1/workouts with "+what, http.StatusOK)
	}
	if res.header.Get("Idempotent-Replayed") != "true" {
		return fmt.Errorf("%s: no Idempotent-Replayed: true header", what)
	}
	w, err := r.written(res)
	if err != nil {
		return err
	}
	if w["id"] != r.id {
		return fmt.Errorf("%s returned workout %v, want %s: it was stored twice", what, w["id"], r.id)
	}
	return nil
}

// update: PATCH changes the title and notes; the result and a read both show the change, and
// updated_at, when the server keeps it, doesn't go backwards.
func (r *runner) update(ctx context.Context) error {
	res, err := r.send(ctx, http.MethodPatch, r.path, map[string]any{"title": newTitle, "notes": newNotes}, nil, true)
	if err != nil {
		return err
	}
	if res.status != http.StatusOK {
		return res.unexpected("PATCH "+r.path, http.StatusOK)
	}
	written, err := r.written(res)
	if err != nil {
		return err
	}
	w, err := r.get(ctx)
	if err != nil {
		return err
	}
	for _, doc := range []map[string]any{written, w} {
		if doc["title"] != newTitle || doc["notes"] != newNotes {
			return fmt.Errorf("after PATCH: title %v and notes %v, want %q and %q", doc["title"], doc["notes"], newTitle, newNotes)
		}
	}
	before, _ := r.stored["updated_at"].(string)
	after, _ := w["updated_at"].(string)
	if before != "" && after != "" {
		b, errB := time.Parse(time.RFC3339Nano, before)
		a, errA := time.Parse(time.RFC3339Nano, after)
		if errB != nil || errA != nil || a.Before(b) {
			return fmt.Errorf("updated_at went from %s to %s", before, after)
		}
	}
	return nil
}

// export: GET /v1/export?format=json returns the log, every workout in it valid, this one
// included with its change.
func (r *runner) export(ctx context.Context) error {
	res, err := r.send(ctx, http.MethodGet, "/v1/export?format=json", nil, nil, true)
	if err != nil {
		return err
	}
	if res.status != http.StatusOK {
		return res.unexpected("GET /v1/export?format=json", http.StatusOK)
	}
	var log struct {
		Workouts []json.RawMessage `json:"workouts"`
	}
	if err := json.Unmarshal(res.body, &log); err != nil {
		return fmt.Errorf("export isn't a JSON object with workouts: %v", err)
	}
	found := false
	var invalid []string
	for i, raw := range log.Workouts {
		if p := r.v.ValidateAs("workout", raw); len(p) > 0 {
			invalid = append(invalid, fmt.Sprintf("workouts[%d]: %s", i, p[0]))
			continue
		}
		var w map[string]any
		_ = json.Unmarshal(raw, &w)
		if w["id"] == r.id {
			found = true
			if w["title"] != newTitle {
				return fmt.Errorf("the export holds this run's workout with title %v, want %q", w["title"], newTitle)
			}
		}
	}
	if len(invalid) > 0 {
		return fmt.Errorf("%d of %d exported workouts aren't valid OWS; first: %s", len(invalid), len(log.Workouts), invalid[0])
	}
	if !found {
		return fmt.Errorf("the export's %d workouts don't include %s", len(log.Workouts), r.id)
	}
	return nil
}

// delete: DELETE removes the workout; reading it is then a not_found problem.
func (r *runner) delete(ctx context.Context) error {
	res, err := r.send(ctx, http.MethodDelete, r.path, nil, nil, true)
	if err != nil {
		return err
	}
	if res.status != http.StatusOK && res.status != http.StatusNoContent {
		return res.unexpected("DELETE "+r.path, http.StatusOK)
	}
	r.deleted = true
	res, err = r.send(ctx, http.MethodGet, r.path, nil, nil, true)
	if err != nil {
		return err
	}
	return res.problem("GET a deleted workout", http.StatusNotFound, "not_found")
}

// stays-deleted: logging the same external_id again, as a sync would after the person deleted
// it, doesn't bring it back.
func (r *runner) staysDeleted(ctx context.Context) error {
	res, err := r.send(ctx, http.MethodPost, "/v1/workouts", r.input, header("Idempotency-Key", "ows-conformance-"+random()), true)
	if err != nil {
		return err
	}
	return res.problem("POST a deleted workout's external_id", http.StatusNotFound, "not_found")
}

// cleanUp deletes the workout if a failed check left it stored.
func (r *runner) cleanUp(ctx context.Context) {
	if r.id != "" && !r.deleted {
		_, _ = r.send(ctx, http.MethodDelete, r.path, nil, nil, true)
	}
}

// get reads the workout: 200 with a valid OWS document.
func (r *runner) get(ctx context.Context) (map[string]any, error) {
	res, err := r.send(ctx, http.MethodGet, r.path, nil, nil, true)
	if err != nil {
		return nil, err
	}
	if res.status != http.StatusOK {
		return nil, res.unexpected("GET "+r.path, http.StatusOK)
	}
	if err := res.isJSON(); err != nil {
		return nil, err
	}
	if p := r.v.ValidateAs("workout", res.body); len(p) > 0 {
		return nil, fmt.Errorf("GET %s isn't a valid OWS workout: %s", r.path, p[0])
	}
	var w map[string]any
	if err := json.Unmarshal(res.body, &w); err != nil {
		return nil, err
	}
	return w, nil
}

// written reads a write's result: a JSON object whose workout is a valid OWS document.
func (r *runner) written(res *response) (map[string]any, error) {
	if err := res.isJSON(); err != nil {
		return nil, err
	}
	var body struct {
		Workout json.RawMessage `json:"workout"`
	}
	if err := json.Unmarshal(res.body, &body); err != nil || len(body.Workout) == 0 {
		return nil, fmt.Errorf("the result has no workout: %s", clip(res.body))
	}
	if p := r.v.ValidateAs("workout", body.Workout); len(p) > 0 {
		return nil, fmt.Errorf("the result's workout isn't valid OWS: %s", p[0])
	}
	var w map[string]any
	if err := json.Unmarshal(body.Workout, &w); err != nil {
		return nil, err
	}
	return w, nil
}

// matchesInput checks the stored workout holds what was sent: start, title, notes, external ID,
// and each exercise's name and sets in order, loads in the unit given.
func matchesInput(w, in map[string]any) error {
	start, err := time.Parse(time.RFC3339Nano, fmt.Sprint(w["started_at"]))
	want, _ := time.Parse(time.RFC3339, in["started_at"].(string))
	if err != nil || !start.Equal(want) {
		return fmt.Errorf("started_at %v, want %s", w["started_at"], in["started_at"])
	}
	for _, f := range []string{"title", "notes"} {
		if w[f] != in[f] {
			return fmt.Errorf("%s %v, want %q", f, w[f], in[f])
		}
	}
	src, _ := w["source"].(map[string]any)
	if src["external_id"] != in["external_id"] {
		return fmt.Errorf("source.external_id %v, want %q", src["external_id"], in["external_id"])
	}
	entries, _ := w["exercises"].([]any)
	sent := in["exercises"].([]any)
	if len(entries) != len(sent) {
		return fmt.Errorf("%d exercises stored, want %d", len(entries), len(sent))
	}
	for i := range sent {
		e, s := entries[i].(map[string]any), sent[i].(map[string]any)
		name := s["name"].(string)
		if text, ok := e["text"].(string); ok && !strings.EqualFold(strings.TrimSpace(text), name) {
			return fmt.Errorf("exercises[%d].text %q, want %q", i, text, name)
		}
		sets, _ := e["sets"].([]any)
		sentSets := s["sets"].([]any)
		if len(sets) != len(sentSets) {
			return fmt.Errorf("exercises[%d]: %d sets stored, want %d", i, len(sets), len(sentSets))
		}
		for j := range sentSets {
			got, want := sets[j].(map[string]any), sentSets[j].(map[string]any)
			if fmt.Sprint(got["reps"]) != fmt.Sprint(want["reps"]) {
				return fmt.Errorf("exercises[%d].sets[%d].reps %v, want %v", i, j, got["reps"], want["reps"])
			}
			load, _ := got["load"].(map[string]any)
			if want["load"] == nil {
				if load != nil {
					return fmt.Errorf("exercises[%d].sets[%d] has a load %v, but none was sent", i, j, load)
				}
				continue
			}
			if load == nil || fmt.Sprint(load["value"]) != fmt.Sprint(want["load"]) || load["unit"] != want["unit"] {
				return fmt.Errorf("exercises[%d].sets[%d].load %v, want %v %v", i, j, got["load"], want["load"], want["unit"])
			}
		}
	}
	return nil
}

// stable drops what may differ between two reads of an unchanged workout: nulls, which a
// server may write or leave out, and updated_at.
func stable(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, x := range t {
			if x == nil || k == "updated_at" {
				continue
			}
			out[k] = stable(x)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = stable(x)
		}
		return out
	}
	return v
}

// differ names the first place a and b differ, or "".
func differ(a, b any) string {
	if reflect.DeepEqual(a, b) {
		return ""
	}
	return walk("", a, b)
}

func walk(at string, a, b any) string {
	am, aok := a.(map[string]any)
	bm, bok := b.(map[string]any)
	if aok && bok {
		for k := range am {
			if d := walk(at+"/"+k, am[k], bm[k]); d != "" {
				return d
			}
		}
		for k := range bm {
			if _, ok := am[k]; !ok {
				return fmt.Sprintf("%s/%s only in the read", at, k)
			}
		}
		return ""
	}
	as, aok := a.([]any)
	bs, bok := b.([]any)
	if aok && bok {
		if len(as) != len(bs) {
			return fmt.Sprintf("%s has %d items, then %d", at, len(as), len(bs))
		}
		for i := range as {
			if d := walk(fmt.Sprintf("%s/%d", at, i), as[i], bs[i]); d != "" {
				return d
			}
		}
		return ""
	}
	if !reflect.DeepEqual(a, b) {
		return fmt.Sprintf("%s was %v, then %v", at, a, b)
	}
	return ""
}

type response struct {
	status int
	header http.Header
	body   []byte
}

// unexpected describes a status other than want, with the problem's code when there is one.
func (res *response) unexpected(what string, want int) error {
	var p struct {
		Code   string `json:"code"`
		Detail string `json:"detail"`
		Error  string `json:"error"`
	}
	_ = json.Unmarshal(res.body, &p)
	msg := fmt.Sprintf("%s: status %d, want %d", what, res.status, want)
	if code := p.Code + p.Error; code != "" {
		msg += " (" + code
		if p.Detail != "" {
			msg += ": " + p.Detail
		}
		msg += ")"
	}
	if res.status == http.StatusTooManyRequests {
		msg += "; rate limited, Retry-After " + res.header.Get("Retry-After")
	}
	return fmt.Errorf("%s", msg)
}

func (res *response) isJSON() error {
	ctype, _, _ := mime.ParseMediaType(res.header.Get("Content-Type"))
	if ctype != "application/json" {
		return fmt.Errorf("Content-Type %q, want application/json", res.header.Get("Content-Type"))
	}
	return nil
}

// problem checks an RFC 9457 problem with the status and code.
func (res *response) problem(what string, status int, code string) error {
	if res.status != status {
		return res.unexpected(what, status)
	}
	ctype, _, _ := mime.ParseMediaType(res.header.Get("Content-Type"))
	if ctype != "application/problem+json" {
		return fmt.Errorf("%s: Content-Type %q, want application/problem+json", what, res.header.Get("Content-Type"))
	}
	var p struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(res.body, &p); err != nil || p.Code != code {
		return fmt.Errorf("%s: problem code %q, want %q", what, p.Code, code)
	}
	return nil
}

// send makes one request. A transport error is the check's failure.
func (r *runner) send(ctx context.Context, method, path string, body any, h http.Header, auth bool) (*response, error) {
	target, err := r.base.Parse(path)
	if err != nil {
		return nil, err
	}
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), rd)
	if err != nil {
		return nil, err
	}
	for k, vs := range h {
		req.Header[k] = vs
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ows-conformance/0")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth {
		req.Header.Set("Authorization", "Bearer "+r.cfg.Token)
	}
	res, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("%s %s: reading the reply: %w", method, path, err)
	}
	return &response{status: res.StatusCode, header: res.Header, body: raw}, nil
}

func header(k, v string) http.Header { return http.Header{k: {v}} }

func random() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func clip(b []byte) string {
	if len(b) > 200 {
		return string(b[:200]) + "…"
	}
	return string(b)
}
