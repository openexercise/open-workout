package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// The Open Workout Schema shapes this server stores and returns (schema/v0/workout.json).

// Load is a weight in the unit the person gave it.
type Load struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

// Set is one set. Estimated and UnitAssumed become JSON pointers in the workout's estimates.
type Set struct {
	ID           string   `json:"id"`
	Type         string   `json:"type,omitempty"`
	Reps         *int     `json:"reps,omitempty"`
	Load         *Load    `json:"load,omitempty"`
	RPE          *float64 `json:"rpe,omitempty"`
	DurationS    *int     `json:"duration_s,omitempty"`
	DistanceM    *float64 `json:"distance_m,omitempty"`
	Calories     *int     `json:"calories,omitempty"`
	HeartRateBPM *int     `json:"heart_rate_bpm,omitempty"`

	Estimated   bool `json:"-"`
	UnitAssumed bool `json:"-"`
}

func (st *Set) isWarmup() bool { return st.Type == "warmup" }

// ExerciseRef is what an entry was matched to. catalogue is confirmed; close and unmatched
// need the person to confirm and never count toward records.
type ExerciseRef struct {
	ID    *string `json:"id"`
	Name  string  `json:"name"`
	Match string  `json:"match"`
}

// Entry is one exercise in a workout.
type Entry struct {
	ID       string      `json:"id"`
	Exercise ExerciseRef `json:"exercise"`
	Text     string      `json:"text,omitempty"`
	Notes    string      `json:"notes,omitempty"`
	Sets     []*Set      `json:"sets"`

	candidates []*CatalogueExercise
}

func (e *Entry) confirmedID() string {
	if e.Exercise.Match == "catalogue" && e.Exercise.ID != nil {
		return *e.Exercise.ID
	}
	return ""
}

// Source says where a workout came from.
type Source struct {
	Kind   string `json:"kind"`
	Client string `json:"client,omitempty"`
}

// Workout is one OWS workout.
type Workout struct {
	Schema    string     `json:"$schema"`
	ID        string     `json:"id"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	UpdatedAt time.Time  `json:"updated_at"`
	Title     string     `json:"title,omitempty"`
	Notes     string     `json:"notes,omitempty"`
	Source    Source     `json:"source"`
	Exercises []*Entry   `json:"exercises"`
	Estimates []string   `json:"estimates"`
}

const workoutSchema = "https://openworkout.dev/schema/v0/workout.json"

// view is a copy of the workout for a result, with its estimates as JSON pointers worked out
// now, since positions shift as entries and sets come and go.
func (w *Workout) view() Workout {
	out := *w
	out.Exercises = make([]*Entry, len(w.Exercises))
	out.Estimates = []string{}
	for i, e := range w.Exercises {
		ce := *e
		ce.Sets = make([]*Set, len(e.Sets))
		for j, st := range e.Sets {
			cs := *st
			ce.Sets[j] = &cs
			if st.Estimated {
				out.Estimates = append(out.Estimates, fmt.Sprintf("/exercises/%d/sets/%d", i, j))
			}
			if st.UnitAssumed && st.Load != nil {
				out.Estimates = append(out.Estimates, fmt.Sprintf("/exercises/%d/sets/%d/load/unit", i, j))
			}
		}
		out.Exercises[i] = &ce
	}
	return out
}

// ToolError is a failure the assistant can act on: its text starts with a profile error code.
type ToolError struct {
	Code    string
	Message string
}

func (e *ToolError) Error() string { return e.Code + ": " + e.Message }

func fail(code, format string, args ...any) error {
	return &ToolError{Code: code, Message: fmt.Sprintf(format, args...)}
}

func invalid(field, format string, args ...any) error {
	return fail("invalid_input", "%s: %s", field, fmt.Sprintf(format, args...))
}

// Store keeps every person's log in memory. A real server keeps it in a database, scoped by
// person in every query, and writes each change in one transaction.
type Store struct {
	mu        sync.Mutex
	catalogue *Catalogue
	unit      string // the unit a load without one is assumed to be in
	loc       *time.Location
	now       func() time.Time
	people    map[string]*personLog
}

// NewStore makes an empty store. unit is "kg" or "lb".
func NewStore(c *Catalogue, unit string, loc *time.Location) *Store {
	return &Store{catalogue: c, unit: unit, loc: loc, now: time.Now, people: map[string]*personLog{}}
}

type personLog struct {
	s        *Store
	workouts []*Workout
	idem     map[string]written
	undo     map[string]undoable
}

// written is a write's first result, replayed for a retry with the same idempotency key.
type written struct {
	fingerprint string
	result      Result
}

// Result is what a tool returns: a summary to echo, then the value as JSON.
type Result interface{ summary() string }

func (s *Store) person(id string) *personLog {
	l, ok := s.people[id]
	if !ok {
		l = &personLog{s: s, idem: map[string]written{}, undo: map[string]undoable{}}
		s.people[id] = l
	}
	return l
}

// Write runs a change to one person's log. With an idempotency key, a retry with the same
// fingerprint (the tool and its arguments) replays the first result instead of writing again,
// and a different fingerprint is refused.
func (s *Store) Write(person, key, fingerprint string, fn func(l *personLog) (Result, error)) (Result, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l := s.person(person)
	if key != "" {
		if prior, ok := l.idem[key]; ok {
			if prior.fingerprint != fingerprint {
				return nil, false, fail("idempotency_conflict", "this idempotency_key was already used for a different request; use a new key")
			}
			return prior.result, true, nil
		}
	}
	res, err := fn(l)
	if err != nil {
		return nil, false, err
	}
	if key != "" {
		l.idem[key] = written{fingerprint, res}
	}
	return res, false, nil
}

// Read runs a read of one person's log.
func (s *Store) Read(person string, fn func(l *personLog) (Result, error)) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(s.person(person))
}

func newID(prefix string) string {
	return prefix + strings.ToLower(rand.Text()[:16])
}

// find returns the workout, entry and set an ID names; kind is "workout", "entry" or "set".
func (l *personLog) find(id string) (kind string, w *Workout, e *Entry, st *Set) {
	for _, w := range l.workouts {
		if w.ID == id {
			return "workout", w, nil, nil
		}
		for _, e := range w.Exercises {
			if e.ID == id {
				return "entry", w, e, nil
			}
			for _, st := range e.Sets {
				if st.ID == id {
					return "set", w, e, st
				}
			}
		}
	}
	return "", nil, nil, nil
}

func notFound() error {
	return fail("not_found", "no workout, exercise entry or set with that ID in this log; IDs come from earlier results or get_workouts")
}

// --- Writing ---

// SetInput is a set as the tools take it.
type SetInput struct {
	Reps         *int     `json:"reps"`
	Load         *float64 `json:"load"`
	Unit         string   `json:"unit"`
	RPE          *float64 `json:"rpe"`
	DurationS    *int     `json:"duration_s"`
	DistanceM    *float64 `json:"distance_m"`
	Calories     *int     `json:"calories"`
	HeartRateBPM *int     `json:"heart_rate_bpm"`
	Warmup       bool     `json:"warmup"`
	Estimated    bool     `json:"estimated"`
}

func (l *personLog) newSet(field string, in SetInput) (*Set, error) {
	if in.Reps == nil && in.Load == nil && in.DurationS == nil && in.DistanceM == nil {
		return nil, invalid(field, "give reps, load, duration_s or distance_m")
	}
	if err := checkNumbers(field, in.Reps, in.Load, in.RPE, in.DurationS, in.DistanceM, in.Calories, in.HeartRateBPM); err != nil {
		return nil, err
	}
	st := &Set{ID: newID("st_"), Reps: in.Reps, RPE: in.RPE, DurationS: in.DurationS, DistanceM: in.DistanceM,
		Calories: in.Calories, HeartRateBPM: in.HeartRateBPM, Estimated: in.Estimated}
	if in.Warmup {
		st.Type = "warmup"
	}
	if in.Load != nil {
		unit, assumed, err := l.unitFor(field, in.Unit)
		if err != nil {
			return nil, err
		}
		st.Load = &Load{Value: *in.Load, Unit: unit}
		st.UnitAssumed = assumed
	}
	return st, nil
}

func checkNumbers(field string, reps *int, load, rpe *float64, duration *int, distance *float64, calories, hr *int) error {
	for name, v := range map[string]*int{"reps": reps, "duration_s": duration, "calories": calories, "heart_rate_bpm": hr} {
		if v != nil && *v < 0 {
			return invalid(field+"."+name, "can't be negative")
		}
	}
	for name, v := range map[string]*float64{"load": load, "distance_m": distance} {
		if v != nil && *v < 0 {
			return invalid(field+"."+name, "can't be negative")
		}
	}
	if rpe != nil && (*rpe < 1 || *rpe > 10) {
		return invalid(field+".rpe", "use 1 to 10")
	}
	return nil
}

func (l *personLog) unitFor(field, unit string) (string, bool, error) {
	switch unit {
	case "":
		return l.s.unit, true, nil
	case "kg", "lb":
		return unit, false, nil
	}
	return "", false, invalid(field+".unit", "use kg or lb")
}

// newEntry matches an exercise: an ID the caller is sure of, else the name against the
// catalogue. A name that isn't an exact match is stored as said, for the person to confirm.
func (l *personLog) newEntry(field, name, exerciseID string) (*Entry, error) {
	name = strings.TrimSpace(name)
	e := &Entry{ID: newID("en_"), Text: name}
	if exerciseID != "" {
		ex := l.s.catalogue.ByID(exerciseID)
		if ex == nil {
			return nil, invalid(field+".exercise_id", "unknown exercise ID; find one with search_exercises")
		}
		e.Exercise = ExerciseRef{ID: &ex.ID, Name: ex.Name, Match: "catalogue"}
		return e, nil
	}
	if name == "" {
		return nil, invalid(field+".name", "name the exercise")
	}
	exact, candidates := l.s.catalogue.Match(name)
	switch {
	case exact != nil:
		e.Exercise = ExerciseRef{ID: &exact.ID, Name: exact.Name, Match: "catalogue"}
	case len(candidates) > 0:
		e.Exercise = ExerciseRef{ID: &candidates[0].ID, Name: candidates[0].Name, Match: "close"}
		e.candidates = candidates
	default:
		e.Exercise = ExerciseRef{Name: name, Match: "unmatched"}
	}
	return e, nil
}

// ExerciseInput is one exercise of log_workout.
type ExerciseInput struct {
	Name       string     `json:"name"`
	ExerciseID string     `json:"exercise_id"`
	Notes      string     `json:"notes"`
	Sets       []SetInput `json:"sets"`
}

// LogWorkoutInput is log_workout's arguments.
type LogWorkoutInput struct {
	Date           string          `json:"date"`
	Title          string          `json:"title"`
	Notes          string          `json:"notes"`
	Exercises      []ExerciseInput `json:"exercises"`
	Program        json.RawMessage `json:"program"`
	IdempotencyKey string          `json:"idempotency_key"`
}

// LogWorkout stores a whole workout.
func (l *personLog) LogWorkout(in LogWorkoutInput, client string) (Result, error) {
	if len(in.Program) > 0 {
		return nil, invalid("program", "this server doesn't offer programs; leave it out")
	}
	if len(in.Exercises) == 0 {
		return nil, invalid("exercises", "give at least one exercise")
	}
	started, err := l.startOf(in.Date)
	if err != nil {
		return nil, err
	}
	w := &Workout{Schema: workoutSchema, ID: newID("wk_"), StartedAt: started, Title: in.Title, Notes: in.Notes,
		Source: Source{Kind: "mcp", Client: client}}
	for i, ex := range in.Exercises {
		field := fmt.Sprintf("exercises[%d]", i)
		e, err := l.newEntry(field, ex.Name, ex.ExerciseID)
		if err != nil {
			return nil, err
		}
		e.Notes = ex.Notes
		if len(ex.Sets) == 0 {
			return nil, invalid(field+".sets", "give at least one set")
		}
		for j, si := range ex.Sets {
			st, err := l.newSet(fmt.Sprintf("%s.sets[%d]", field, j), si)
			if err != nil {
				return nil, err
			}
			e.Sets = append(e.Sets, st)
		}
		w.Exercises = append(w.Exercises, e)
	}
	before := l.records()
	w.UpdatedAt = l.s.now()
	l.workouts = append(l.workouts, w)
	return l.logged(w, "Logged", before), nil
}

// startOf is when a workout on a day started: now for today, else midday that day.
func (l *personLog) startOf(date string) (time.Time, error) {
	now := l.s.now().In(l.s.loc)
	if date == "" {
		return now, nil
	}
	day, err := time.ParseInLocation(time.DateOnly, date, l.s.loc)
	if err != nil {
		return time.Time{}, invalid("date", "use YYYY-MM-DD")
	}
	today := now.Format(time.DateOnly)
	switch {
	case date == today:
		return now, nil
	case date > today:
		return time.Time{}, invalid("date", "that day hasn't happened yet")
	}
	return day.Add(12 * time.Hour), nil
}

// AddSetsInput is add_sets' arguments.
type AddSetsInput struct {
	WorkoutID      string     `json:"workout_id"`
	EntryID        string     `json:"entry_id"`
	Exercise       string     `json:"exercise"`
	ExerciseID     string     `json:"exercise_id"`
	Sets           []SetInput `json:"sets"`
	IdempotencyKey string     `json:"idempotency_key"`
}

// AddSets adds sets to an entry, or to an exercise in a workout: by default the session in
// progress (unfinished, started in the last 4 hours), starting one if there's none.
func (l *personLog) AddSets(in AddSetsInput, client string) (Result, error) {
	if len(in.Sets) == 0 {
		return nil, invalid("sets", "give at least one set")
	}
	sets := make([]*Set, 0, len(in.Sets))
	for i, si := range in.Sets {
		st, err := l.newSet(fmt.Sprintf("sets[%d]", i), si)
		if err != nil {
			return nil, err
		}
		sets = append(sets, st)
	}
	before := l.records()
	w, e, err := l.entryForSets(in, client)
	if err != nil {
		return nil, err
	}
	e.Sets = append(e.Sets, sets...)
	w.UpdatedAt = l.s.now()
	return l.logged(w, "Added", before), nil
}

func (l *personLog) entryForSets(in AddSetsInput, client string) (*Workout, *Entry, error) {
	if in.EntryID != "" {
		kind, w, e, _ := l.find(in.EntryID)
		if kind != "entry" {
			return nil, nil, notFound()
		}
		return w, e, nil
	}
	if in.Exercise == "" && in.ExerciseID == "" {
		return nil, nil, invalid("exercise", "name the exercise, or pass entry_id")
	}
	e, err := l.newEntry("exercise", in.Exercise, in.ExerciseID)
	if err != nil {
		return nil, nil, err
	}
	w, err := l.workoutForSets(in.WorkoutID, client)
	if err != nil {
		return nil, nil, err
	}
	for _, have := range w.Exercises {
		sameExercise := e.confirmedID() != "" && have.confirmedID() == e.confirmedID()
		sameWords := e.confirmedID() == "" && normalize(have.Text) == normalize(e.Text)
		if sameExercise || sameWords {
			return w, have, nil
		}
	}
	w.Exercises = append(w.Exercises, e)
	return w, e, nil
}

func (l *personLog) workoutForSets(id, client string) (*Workout, error) {
	if id != "" && id != "current" {
		kind, w, _, _ := l.find(id)
		if kind != "workout" {
			return nil, notFound()
		}
		return w, nil
	}
	now := l.s.now()
	var current *Workout
	for _, w := range l.workouts {
		if w.EndedAt == nil && now.Sub(w.StartedAt) < 4*time.Hour && (current == nil || w.StartedAt.After(current.StartedAt)) {
			current = w
		}
	}
	if current != nil {
		return current, nil
	}
	w := &Workout{Schema: workoutSchema, ID: newID("wk_"), StartedAt: now.In(l.s.loc), Source: Source{Kind: "mcp", Client: client}}
	l.workouts = append(l.workouts, w)
	return w, nil
}

// UpdateInput is update_entry's arguments: which fields apply depends on what entry_id names.
type UpdateInput struct {
	EntryID      string   `json:"entry_id"`
	Title        *string  `json:"title"`
	Date         *string  `json:"date"`
	Notes        *string  `json:"notes"`
	EndedAt      *string  `json:"ended_at"`
	ExerciseID   *string  `json:"exercise_id"`
	Reps         *int     `json:"reps"`
	Load         *float64 `json:"load"`
	RemoveLoad   *bool    `json:"remove_load"`
	Unit         *string  `json:"unit"`
	RPE          *float64 `json:"rpe"`
	DurationS    *int     `json:"duration_s"`
	DistanceM    *float64 `json:"distance_m"`
	Calories     *int     `json:"calories"`
	HeartRateBPM *int     `json:"heart_rate_bpm"`
	Warmup       *bool    `json:"warmup"`
}

// fields lists the fields given, by name, for checking them against what entry_id names.
func (in UpdateInput) fields() map[string]bool {
	given := map[string]bool{}
	add := func(name string, isSet bool) {
		if isSet {
			given[name] = true
		}
	}
	add("title", in.Title != nil)
	add("date", in.Date != nil)
	add("notes", in.Notes != nil)
	add("ended_at", in.EndedAt != nil)
	add("exercise_id", in.ExerciseID != nil)
	add("reps", in.Reps != nil)
	add("load", in.Load != nil)
	add("remove_load", in.RemoveLoad != nil)
	add("unit", in.Unit != nil)
	add("rpe", in.RPE != nil)
	add("duration_s", in.DurationS != nil)
	add("distance_m", in.DistanceM != nil)
	add("calories", in.Calories != nil)
	add("heart_rate_bpm", in.HeartRateBPM != nil)
	add("warmup", in.Warmup != nil)
	return given
}

var fieldsOf = map[string][]string{
	"workout": {"title", "date", "notes", "ended_at"},
	"entry":   {"exercise_id", "notes"},
	"set":     {"reps", "load", "remove_load", "unit", "rpe", "duration_s", "distance_m", "calories", "heart_rate_bpm", "warmup"},
}

// Update changes a workout, an entry or a set.
func (l *personLog) Update(in UpdateInput) (Result, error) {
	kind, w, e, st := l.find(in.EntryID)
	if kind == "" {
		return nil, notFound()
	}
	given := in.fields()
	if len(given) == 0 {
		return nil, invalid("entry_id", "give at least one field to change")
	}
	for name := range given {
		if !slices.Contains(fieldsOf[kind], name) {
			return nil, invalid(name, "a %s has no such field; a %s takes %s", kind, kind, strings.Join(fieldsOf[kind], ", "))
		}
	}
	before := l.records()
	var err error
	switch kind {
	case "workout":
		err = l.updateWorkout(w, in)
	case "entry":
		err = l.updateEntry(e, in)
	case "set":
		err = l.updateSet(st, in)
	}
	if err != nil {
		return nil, err
	}
	w.UpdatedAt = l.s.now()
	return l.logged(w, "Updated", before), nil
}

func (l *personLog) updateWorkout(w *Workout, in UpdateInput) error {
	if in.Date != nil {
		day, err := l.startOf(*in.Date)
		if err != nil {
			return err
		}
		clock := w.StartedAt.In(l.s.loc)
		started := time.Date(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), clock.Second(), 0, l.s.loc)
		if w.EndedAt != nil {
			ended := w.EndedAt.Add(started.Sub(w.StartedAt))
			w.EndedAt = &ended
		}
		w.StartedAt = started
	}
	if in.EndedAt != nil {
		ended, err := time.Parse(time.RFC3339, *in.EndedAt)
		if err != nil {
			return invalid("ended_at", "use RFC 3339, e.g. 2026-10-28T18:45:00-05:00")
		}
		if ended.Before(w.StartedAt) || ended.After(l.s.now().Add(5*time.Minute)) {
			return invalid("ended_at", "must be after the workout started and not in the future")
		}
		w.EndedAt = &ended
	}
	if in.Title != nil {
		w.Title = *in.Title
	}
	if in.Notes != nil {
		w.Notes = *in.Notes
	}
	return nil
}

func (l *personLog) updateEntry(e *Entry, in UpdateInput) error {
	if in.ExerciseID != nil {
		ex := l.s.catalogue.ByID(*in.ExerciseID)
		if ex == nil {
			return invalid("exercise_id", "unknown exercise ID; find one with search_exercises")
		}
		e.Exercise = ExerciseRef{ID: &ex.ID, Name: ex.Name, Match: "catalogue"}
		e.candidates = nil
	}
	if in.Notes != nil {
		e.Notes = *in.Notes
	}
	return nil
}

func (l *personLog) updateSet(st *Set, in UpdateInput) error {
	if err := checkNumbers("set", in.Reps, in.Load, in.RPE, in.DurationS, in.DistanceM, in.Calories, in.HeartRateBPM); err != nil {
		return err
	}
	if in.Unit != nil {
		if *in.Unit != "kg" && *in.Unit != "lb" {
			return invalid("unit", "use kg or lb")
		}
		if st.Load == nil && in.Load == nil {
			return invalid("unit", "this set has no load; give load with the unit")
		}
	}
	switch {
	case in.RemoveLoad != nil && *in.RemoveLoad:
		st.Load, st.UnitAssumed = nil, false
	case in.Load != nil && st.Load != nil:
		st.Load.Value = *in.Load
	case in.Load != nil:
		st.Load = &Load{Value: *in.Load, Unit: l.s.unit}
		st.UnitAssumed = true
	}
	if in.Unit != nil && st.Load != nil {
		st.Load.Unit, st.UnitAssumed = *in.Unit, false
	}
	if in.Reps != nil {
		st.Reps = in.Reps
	}
	if in.RPE != nil {
		st.RPE = in.RPE
	}
	if in.DurationS != nil {
		st.DurationS = in.DurationS
	}
	if in.DistanceM != nil {
		st.DistanceM = in.DistanceM
	}
	if in.Calories != nil {
		st.Calories = in.Calories
	}
	if in.HeartRateBPM != nil {
		st.HeartRateBPM = in.HeartRateBPM
	}
	if in.Warmup != nil {
		st.Type = ""
		if *in.Warmup {
			st.Type = "warmup"
		}
	}
	st.Estimated = false // the person has now said what it was
	return nil
}

// --- Deleting and undoing ---

const undoWindow = 10 * time.Minute

// undoable is what delete_entry removed, with where it was, until its token expires.
type undoable struct {
	kind      string
	workout   *Workout
	entry     *Entry
	set       *Set
	parentID  string // the workout an entry was in, or the entry a set was in
	position  int
	expiresAt time.Time
}

// Deleted is delete_entry's result.
type Deleted struct {
	Kind           string    `json:"kind"`
	ID             string    `json:"id"`
	UndoToken      string    `json:"undo_token"`
	ExpiresAt      time.Time `json:"undo_expires_at"`
	RecordsChanged []Record  `json:"records_changed"`
	Summary        string    `json:"summary"`
}

func (d Deleted) summary() string { return d.Summary }

// Delete removes a workout, an entry with its sets, or a set, and returns a token to undo it.
func (l *personLog) Delete(id string) (Result, error) {
	kind, w, e, st := l.find(id)
	if kind == "" {
		return nil, notFound()
	}
	before := l.records()
	u := undoable{kind: kind, expiresAt: l.s.now().Add(undoWindow)}
	switch kind {
	case "workout":
		u.workout = w
		l.workouts = slices.DeleteFunc(l.workouts, func(x *Workout) bool { return x == w })
	case "entry":
		u.entry, u.parentID, u.position = e, w.ID, slices.Index(w.Exercises, e)
		w.Exercises = slices.Delete(w.Exercises, u.position, u.position+1)
		w.UpdatedAt = l.s.now()
	case "set":
		u.set, u.parentID, u.position = st, e.ID, slices.Index(e.Sets, st)
		e.Sets = slices.Delete(e.Sets, u.position, u.position+1)
		w.UpdatedAt = l.s.now()
	}
	token := newID("undo_")
	l.undo[token] = u
	return Deleted{
		Kind: kind, ID: id, UndoToken: token, ExpiresAt: u.expiresAt,
		RecordsChanged: changedRecords(before, l.records()),
		Summary:        fmt.Sprintf("Deleted the %s. It can be undone for 10 minutes.", kind),
	}, nil
}

// Undo puts back what a delete removed, with its original IDs.
func (l *personLog) Undo(token string) (Result, error) {
	u, ok := l.undo[token]
	unavailable := fail("undo_unavailable", "this undo token is unknown, already used or expired (tokens last 10 minutes), or what it restores into was deleted")
	if !ok || l.s.now().After(u.expiresAt) {
		return nil, unavailable
	}
	before := l.records()
	var w *Workout
	switch u.kind {
	case "workout":
		w = u.workout
		l.workouts = append(l.workouts, w)
	case "entry":
		kind, parent, _, _ := l.find(u.parentID)
		if kind != "workout" {
			return nil, unavailable
		}
		w = parent
		w.Exercises = slices.Insert(w.Exercises, min(u.position, len(w.Exercises)), u.entry)
	case "set":
		kind, parent, e, _ := l.find(u.parentID)
		if kind != "entry" {
			return nil, unavailable
		}
		w = parent
		e.Sets = slices.Insert(e.Sets, min(u.position, len(e.Sets)), u.set)
	}
	delete(l.undo, token)
	w.UpdatedAt = l.s.now()
	return l.logged(w, "Restored", before), nil
}

// --- Results of writes ---

// Confirmation is an entry the assistant should confirm with the person, then fix with
// update_entry's exercise_id.
type Confirmation struct {
	EntryID    string      `json:"entry_id"`
	Text       string      `json:"text"`
	Status     string      `json:"status"`
	Candidates []Candidate `json:"candidates"`
}

// Candidate is an exercise an unconfirmed entry may be.
type Candidate struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Logged is the result of a write that leaves a workout: the workout, a summary to echo,
// records that changed and entries to confirm.
type Logged struct {
	Workout           Workout        `json:"workout"`
	Summary           string         `json:"summary"`
	NewRecords        []Record       `json:"new_records"`
	NeedsConfirmation []Confirmation `json:"needs_confirmation"`
}

func (r Logged) summary() string { return r.Summary }

func (l *personLog) logged(w *Workout, verb string, before map[recordKey]Record) Logged {
	changed := changedRecords(before, l.records())
	out := Logged{Workout: w.view(), NewRecords: changed, NeedsConfirmation: []Confirmation{}}
	var names []string
	for _, e := range w.Exercises {
		names = append(names, strings.TrimSpace(entryName(e)+" "+setsLine(e.Sets)))
		if e.Exercise.Match == "catalogue" {
			continue
		}
		c := Confirmation{EntryID: e.ID, Text: e.Text, Status: e.Exercise.Match, Candidates: []Candidate{}}
		for _, ex := range e.candidates {
			c.Candidates = append(c.Candidates, Candidate{ex.ID, ex.Name})
		}
		out.NeedsConfirmation = append(out.NeedsConfirmation, c)
	}
	out.Summary = verb + ": " + strings.Join(names, ", ") + "."
	for _, r := range changed {
		if r.Previous != nil {
			out.Summary += fmt.Sprintf(" New PR: %s %s %s %s (was %s).", r.ExerciseName, recordLabels[r.Kind], number(r.Value), r.Unit, number(*r.Previous))
		}
	}
	if len(out.NeedsConfirmation) > 0 {
		out.Summary += " Some exercises need confirming."
	}
	return out
}

// entryName is how summaries name an entry: unconfirmed ones as the person said them.
func entryName(e *Entry) string {
	if e.Exercise.Match == "catalogue" {
		return e.Exercise.Name
	}
	return fmt.Sprintf("%q", e.Text)
}

func setsLine(sets []*Set) string {
	parts := make([]string, 0, len(sets))
	for _, st := range sets {
		var p []string
		if st.Reps != nil {
			p = append(p, fmt.Sprintf("%d", *st.Reps))
		}
		if st.Load != nil {
			p = append(p, "@ "+number(st.Load.Value)+" "+st.Load.Unit)
		}
		if st.DurationS != nil {
			p = append(p, fmt.Sprintf("%ds", *st.DurationS))
		}
		if st.DistanceM != nil {
			p = append(p, number(*st.DistanceM)+" m")
		}
		parts = append(parts, strings.Join(p, " "))
	}
	return strings.Join(parts, ", ")
}

func number(v float64) string {
	return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.1f", v), "0"), ".")
}
