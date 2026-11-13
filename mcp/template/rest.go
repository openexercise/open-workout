package main

import (
	"bytes"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

// The Open Workout API core (api/core.yaml) over the same store as the MCP tools, so the
// template is a whole Open Workout server to copy: it passes ows conformance as well as ows
// conformance mcp. Every route reads and writes the log of the person the bearer token names.

// rest serves /v1.
type rest struct {
	store *Store
	hooks *Hooks
}

// routes registers the core's operations on mux. Reads accept the read-only credential; writes
// need one that may write.
func (a *rest) routes(mux *http.ServeMux) {
	read := func(h func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc { return a.as(false, h) }
	write := func(h func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc { return a.as(true, h) }

	mux.HandleFunc("GET /v1/workouts", read(a.listWorkouts))
	mux.HandleFunc("POST /v1/workouts", write(a.createWorkout))
	mux.HandleFunc("GET /v1/workouts/{id}", read(a.getWorkout))
	mux.HandleFunc("PATCH /v1/workouts/{id}", write(a.update("workout")))
	mux.HandleFunc("DELETE /v1/workouts/{id}", write(a.delete("workout")))
	mux.HandleFunc("POST /v1/workouts/{id}/sets", write(a.addWorkoutSets))
	mux.HandleFunc("PATCH /v1/entries/{id}", write(a.update("entry")))
	mux.HandleFunc("DELETE /v1/entries/{id}", write(a.delete("entry")))
	mux.HandleFunc("POST /v1/entries/{id}/sets", write(a.addEntrySets))
	mux.HandleFunc("PATCH /v1/sets/{id}", write(a.update("set")))
	mux.HandleFunc("DELETE /v1/sets/{id}", write(a.delete("set")))
	mux.HandleFunc("POST /v1/undo", write(a.undo))
	mux.HandleFunc("GET /v1/webhooks", read(a.listWebhooks))
	mux.HandleFunc("POST /v1/webhooks", write(a.createWebhook))
	mux.HandleFunc("DELETE /v1/webhooks/{id}", write(a.deleteWebhook))
	mux.HandleFunc("GET /v1/export", read(a.export))
	mux.HandleFunc("GET /v1/exercises", read(a.searchExercises))
	mux.HandleFunc("/v1/", func(w http.ResponseWriter, _ *http.Request) { problem(w, fail("not_found", "no such route")) })
}

// as runs a handler for the token's person, refusing a write to a read-only credential (403
// insufficient_scope, as OAuth resource servers answer).
func (a *rest) as(isWrite bool, h func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info := auth.TokenInfoFromContext(r.Context())
		if info == nil {
			problem(w, fail("unauthenticated", "no credential"))
			return
		}
		if isWrite && !slices.Contains(info.Scopes, "mcp") {
			w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope", scope="mcp"`)
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "insufficient_scope", "error_description": "this credential can only read"})
			return
		}
		h(w, r, info.UserID)
	}
}

// --- Workouts ---

func (a *rest) createWorkout(w http.ResponseWriter, r *http.Request, person string) {
	var in LogWorkoutInput
	body, ok := readBody(w, r, &in)
	if !ok {
		return
	}
	if in.IdempotencyKey != "" {
		problem(w, invalid("idempotency_key", "send it as the Idempotency-Key header"))
		return
	}
	a.written(w, r, person, "POST /v1/workouts", body, func(l *personLog) (Result, error) { return l.LogWorkout(in, "api", "") })
}

func (a *rest) getWorkout(w http.ResponseWriter, r *http.Request, person string) {
	res, err := a.store.Read(person, func(l *personLog) (Result, error) {
		kind, wk, _, _ := l.find(r.PathValue("id"))
		if kind != "workout" {
			return nil, notFound()
		}
		return workoutResult(wk.view()), nil
	})
	if err != nil {
		problem(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// workoutResult is a bare workout as a Result.
type workoutResult Workout

func (workoutResult) summary() string { return "" }

func (a *rest) listWorkouts(w http.ResponseWriter, r *http.Request, person string) {
	q := r.URL.Query()
	limit := 20
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 100 {
			problem(w, invalid("limit", "a whole number from 1 to 100"))
			return
		}
		limit = n
	}
	res, err := a.store.Read(person, func(l *personLog) (Result, error) {
		if since := q.Get("updated_since"); since != "" {
			return l.Changes(since, q.Get("include_deleted") == "true", limit, q.Get("cursor"))
		}
		return l.Workouts(q.Get("from"), q.Get("to"), q.Get("exercise"), limit)
	})
	if err != nil {
		problem(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// update serves PATCH on a workout, an entry or a set: the ID must be of the route's kind.
func (a *rest) update(kind string) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, person string) {
		var in UpdateInput
		if _, ok := readBody(w, r, &in); !ok {
			return
		}
		if in.EntryID != "" {
			problem(w, invalid("entry_id", "the ID is in the path"))
			return
		}
		in.EntryID = r.PathValue("id")
		a.written(w, r, person, "", nil, func(l *personLog) (Result, error) {
			if found, _, _, _ := l.find(in.EntryID); found != kind {
				return nil, notFound()
			}
			return l.Update(in)
		})
	}
}

func (a *rest) delete(kind string) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, person string) {
		a.written(w, r, person, "", nil, func(l *personLog) (Result, error) {
			id := r.PathValue("id")
			if found, _, _, _ := l.find(id); found != kind {
				return nil, notFound()
			}
			return l.Delete(id)
		})
	}
}

func (a *rest) addWorkoutSets(w http.ResponseWriter, r *http.Request, person string) {
	var in struct {
		Exercise   string     `json:"exercise"`
		ExerciseID string     `json:"exercise_id"`
		Sets       []SetInput `json:"sets"`
	}
	body, ok := readBody(w, r, &in)
	if !ok {
		return
	}
	id := r.PathValue("id")
	a.written(w, r, person, "POST /v1/workouts/"+id+"/sets", body, func(l *personLog) (Result, error) {
		if id != "current" {
			if found, _, _, _ := l.find(id); found != "workout" {
				return nil, notFound()
			}
		}
		return l.AddSets(AddSetsInput{WorkoutID: id, Exercise: in.Exercise, ExerciseID: in.ExerciseID, Sets: in.Sets}, "api", "")
	})
}

func (a *rest) addEntrySets(w http.ResponseWriter, r *http.Request, person string) {
	var in struct {
		Sets []SetInput `json:"sets"`
	}
	body, ok := readBody(w, r, &in)
	if !ok {
		return
	}
	id := r.PathValue("id")
	a.written(w, r, person, "POST /v1/entries/"+id+"/sets", body, func(l *personLog) (Result, error) {
		if found, _, _, _ := l.find(id); found != "entry" {
			return nil, notFound()
		}
		return l.AddSets(AddSetsInput{EntryID: id, Sets: in.Sets}, "api", "")
	})
}

func (a *rest) undo(w http.ResponseWriter, r *http.Request, person string) {
	var in struct {
		UndoToken string `json:"undo_token"`
	}
	if _, ok := readBody(w, r, &in); !ok {
		return
	}
	a.written(w, r, person, "", nil, func(l *personLog) (Result, error) { return l.Undo(in.UndoToken) })
}

// written runs a write and answers with its result. A create (route set) takes an
// Idempotency-Key header: a retry with the same key and body replays the first result (200,
// Idempotent-Replayed), as does an external_id already stored; a new one answers 201 with
// Location.
func (a *rest) written(w http.ResponseWriter, r *http.Request, person, route string, body []byte, fn func(l *personLog) (Result, error)) {
	key := r.Header.Get("Idempotency-Key")
	if route == "" {
		key = ""
	}
	if len(key) > 200 {
		problem(w, invalid("Idempotency-Key", "at most 200 characters"))
		return
	}
	res, replayed, err := a.store.Write(person, key, route+" "+canonical(body), fn)
	if err != nil {
		problem(w, err)
		return
	}
	logged, isLogged := res.(Logged)
	if isLogged && logged.Replayed {
		replayed = true
	}
	status := http.StatusOK
	switch {
	case replayed:
		w.Header().Set("Idempotent-Replayed", "true")
		if isLogged {
			logged.Replayed = true
			res = logged
		}
	case route != "":
		status = http.StatusCreated
	}
	if isLogged && route != "" {
		w.Header().Set("Location", "/v1/workouts/"+logged.Workout.ID)
	}
	writeJSON(w, status, res)
}

// --- Change feed ---

// ChangeList is the change feed: workouts changed since a time, oldest change first, and the
// deleted ones as tombstones.
type ChangeList struct {
	UpdatedSince string      `json:"updated_since"`
	Workouts     []Workout   `json:"workouts"`
	Deleted      []Tombstone `json:"deleted,omitempty"`
	Totals       Totals      `json:"totals"`
	Truncated    bool        `json:"truncated"`
	NextCursor   string      `json:"next_cursor,omitempty"`
	Summary      string      `json:"summary"`
}

func (c ChangeList) summary() string { return c.Summary }

// Changes lists what changed at or after since, oldest change first, a page at a time: the cursor
// is the (updated_at, id) of the last workout on the page before, so a change made while paging
// moves the workout to the end instead of skipping it. Tombstones come with the first page.
func (l *personLog) Changes(since string, includeDeleted bool, limit int, cursor string) (Result, error) {
	at, err := time.Parse(time.RFC3339, since)
	if err != nil {
		return nil, invalid("updated_since", "an RFC 3339 time, such as 2026-11-01T00:00:00Z")
	}
	var after struct {
		At time.Time `json:"t"`
		ID string    `json:"id"`
	}
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || json.Unmarshal(raw, &after) != nil {
			return nil, invalid("cursor", "next_cursor from the page before")
		}
	}
	isAfterCursor := func(w *Workout) bool {
		if cursor == "" {
			return true
		}
		c := w.UpdatedAt.Compare(after.At)
		return c > 0 || (c == 0 && w.ID > after.ID)
	}
	var changed []*Workout
	for _, w := range l.workouts {
		if !w.UpdatedAt.Before(at) && isAfterCursor(w) {
			changed = append(changed, w)
		}
	}
	slices.SortFunc(changed, func(a, b *Workout) int {
		if c := a.UpdatedAt.Compare(b.UpdatedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	out := ChangeList{UpdatedSince: at.UTC().Format(time.RFC3339Nano), Workouts: []Workout{}, Totals: Totals{Unit: l.s.unit}}
	for _, w := range changed {
		if len(out.Workouts) == limit {
			out.Truncated = true
			last := changed[len(out.Workouts)-1]
			raw, _ := json.Marshal(map[string]any{"t": last.UpdatedAt, "id": last.ID})
			out.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
			break
		}
		out.Workouts = append(out.Workouts, w.view())
		out.Totals.Workouts++
	}
	if includeDeleted && cursor == "" {
		out.Deleted = []Tombstone{}
		for _, t := range l.deleted {
			if !t.UpdatedAt.Before(at) {
				out.Deleted = append(out.Deleted, t)
			}
		}
	}
	out.Summary = fmt.Sprintf("%d changed workouts and %d deleted since %s.", len(out.Workouts), len(out.Deleted), out.UpdatedSince)
	return out, nil
}

// --- Export ---

// export is the whole log as an OWS log document (schema/v0/log.json), or one CSV row per set.
func (a *rest) export(w http.ResponseWriter, r *http.Request, person string) {
	format := r.URL.Query().Get("format")
	if format != "" && format != "json" && format != "csv" {
		problem(w, invalid("format", "json or csv"))
		return
	}
	var workouts []Workout
	_, err := a.store.Read(person, func(l *personLog) (Result, error) {
		sorted := slices.Clone(l.workouts)
		slices.SortFunc(sorted, func(a, b *Workout) int { return b.StartedAt.Compare(a.StartedAt) })
		for _, wk := range sorted {
			workouts = append(workouts, wk.view())
		}
		return nil, nil
	})
	if err != nil {
		problem(w, err)
		return
	}
	if workouts == nil {
		workouts = []Workout{}
	}
	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="workouts.csv"`)
		writeCSV(w, workouts)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="workouts.json"`)
	writeJSON(w, http.StatusOK, map[string]any{
		"$schema": "https://openworkout.dev/schema/v0/log.json", "exported_at": time.Now().UTC(), "workouts": workouts,
	})
}

func writeCSV(w io.Writer, workouts []Workout) {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"workout_id", "started_at", "title", "exercise", "exercise_id", "set", "reps", "load", "unit", "duration_s", "distance_m", "warmup"})
	for _, wk := range workouts {
		for _, e := range wk.Exercises {
			id := ""
			if e.Exercise.ID != nil {
				id = *e.Exercise.ID
			}
			for i, st := range e.Sets {
				row := []string{wk.ID, wk.StartedAt.Format(time.RFC3339), safeCell(wk.Title), safeCell(e.Exercise.Name), id, strconv.Itoa(i + 1),
					optional(st.Reps), "", "", optional(st.DurationS), "", strconv.FormatBool(st.isWarmup())}
				if st.Load != nil {
					row[7], row[8] = number(st.Load.Value), st.Load.Unit
				}
				if st.DistanceM != nil {
					row[10] = number(*st.DistanceM)
				}
				_ = cw.Write(row)
			}
		}
	}
	cw.Flush()
}

// safeCell keeps a spreadsheet from running text as a formula.
func safeCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@", rune(s[0])) {
		return "'" + s
	}
	return s
}

func optional(n *int) string {
	if n == nil {
		return ""
	}
	return strconv.Itoa(*n)
}

// --- Exercises ---

func (a *rest) searchExercises(w http.ResponseWriter, r *http.Request, person string) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	res, err := a.store.Read(person, func(l *personLog) (Result, error) {
		return l.Search(q.Get("query"), q.Get("equipment"), q.Get("muscle"), limit)
	})
	if err != nil {
		problem(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// --- Webhooks ---

func (a *rest) createWebhook(w http.ResponseWriter, r *http.Request, person string) {
	var in struct {
		URL    string   `json:"url"`
		Events []string `json:"events"`
	}
	if _, ok := readBody(w, r, &in); !ok {
		return
	}
	sub, err := a.hooks.Subscribe(person, in.URL, in.Events)
	if err != nil {
		problem(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sub)
}

func (a *rest) listWebhooks(w http.ResponseWriter, _ *http.Request, person string) {
	writeJSON(w, http.StatusOK, map[string]any{"webhooks": a.hooks.List(person)})
}

func (a *rest) deleteWebhook(w http.ResponseWriter, r *http.Request, person string) {
	if !a.hooks.Delete(person, r.PathValue("id")) {
		problem(w, fail("not_found", "no webhook with that ID"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Bodies and problems ---

// readBody decodes a JSON body strictly, as the core's schemas say (no unknown fields), and
// returns its bytes for the idempotency fingerprint.
func readBody(w http.ResponseWriter, r *http.Request, v any) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		problem(w, invalid("body", "at most 1 MB"))
		return nil, false
	}
	stripped, err := withoutExtensions(body)
	if err != nil {
		problem(w, invalid("body", "not valid JSON: %v", err))
		return nil, false
	}
	dec := json.NewDecoder(bytes.NewReader(stripped))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		problem(w, invalid("body", "not valid JSON for this endpoint: %v", err))
		return nil, false
	}
	return body, true
}

// canonical is a body in a stable form, so a retry that orders fields differently is the same
// request.
func canonical(body []byte) string {
	var v any
	if json.Unmarshal(body, &v) != nil {
		return string(body)
	}
	out, _ := json.Marshal(v)
	return string(out)
}

var problemStatuses = map[string]int{
	"invalid_input":        http.StatusBadRequest,
	"exercise_ambiguous":   http.StatusUnprocessableEntity,
	"not_found":            http.StatusNotFound,
	"undo_unavailable":     http.StatusGone,
	"idempotency_conflict": http.StatusUnprocessableEntity,
	"unauthenticated":      http.StatusUnauthorized,
	"internal_error":       http.StatusInternalServerError,
}

// problem answers with an RFC 9457 problem whose code is the profile's error code; field names
// the input at fault for invalid_input.
func problem(w http.ResponseWriter, err error) {
	var te *ToolError
	if !errors.As(err, &te) {
		te = &ToolError{Code: "internal_error", Message: "something went wrong on the server's side; try again"}
	}
	status := problemStatuses[te.Code]
	if status == 0 {
		status = http.StatusBadRequest
	}
	p := map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "code": te.Code, "detail": te.Message}
	if te.Code == "invalid_input" {
		if field, _, ok := strings.Cut(te.Message, ": "); ok {
			p["field"] = field
		}
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// withoutExtensions drops every x- member at any depth, which writes ignore (SPEC.md, section 8),
// so the strict decoding that follows refuses only members nobody defined.
func withoutExtensions(body []byte) ([]byte, error) {
	var doc any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	var strip func(v any)
	strip = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			for k, child := range v {
				if strings.HasPrefix(k, "x-") {
					delete(v, k)
					continue
				}
				strip(child)
			}
		case []any:
			for _, child := range v {
				strip(child)
			}
		}
	}
	strip(doc)
	return json.Marshal(doc)
}
