package main

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

// --- Records ---

// Record is a personal record: the best of one kind for one confirmed exercise, from working
// sets only, in the server's unit.
type Record struct {
	ExerciseID   string   `json:"exercise_id"`
	ExerciseName string   `json:"exercise_name"`
	Kind         string   `json:"kind"`
	Value        float64  `json:"value"`
	Unit         string   `json:"unit"`
	Previous     *float64 `json:"previous,omitempty"`
	AchievedOn   string   `json:"achieved_on"`
	WorkoutID    string   `json:"workout_id"`
	SetID        string   `json:"set_id"`
	Reps         *int     `json:"reps,omitempty"`
	Load         *Load    `json:"load,omitempty"`
}

var recordLabels = map[string]string{
	"heaviest_load":   "heaviest load",
	"best_e1rm":       "estimated 1RM",
	"best_set_volume": "best set volume",
}

type recordKey struct{ exercise, kind string }

const lbInKg = 0.45359237

// inUnit converts a load to unit.
func inUnit(ld Load, unit string) float64 {
	switch {
	case ld.Unit == unit:
		return ld.Value
	case unit == "kg":
		return ld.Value * lbInKg
	}
	return ld.Value / lbInKg
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// e1rm is Epley's estimate, counted for 1 to 12 reps only.
func e1rm(load float64, reps int) (float64, bool) {
	if reps < 1 || reps > 12 {
		return 0, false
	}
	return load * (1 + float64(reps)/30), true
}

// records works out every record from the whole log. A real server keeps them in a table and
// rebuilds an exercise's records when its sets change.
func (l *personLog) records() map[recordKey]Record {
	out := map[recordKey]Record{}
	byTime := slices.Clone(l.workouts)
	slices.SortFunc(byTime, func(a, b *Workout) int { return a.StartedAt.Compare(b.StartedAt) })
	for _, w := range byTime {
		for _, e := range w.Exercises {
			id := e.confirmedID()
			if id == "" {
				continue
			}
			for _, st := range e.Sets {
				if st.isWarmup() || st.Load == nil || st.Reps == nil || *st.Reps < 1 {
					continue
				}
				load := inUnit(*st.Load, l.s.unit)
				values := map[string]float64{"heaviest_load": load, "best_set_volume": load * float64(*st.Reps)}
				if v, ok := e1rm(load, *st.Reps); ok {
					values["best_e1rm"] = v
				}
				for kind, v := range values {
					k := recordKey{id, kind}
					if have, ok := out[k]; ok && round1(v) <= have.Value {
						continue
					}
					ld := *st.Load
					out[k] = Record{ExerciseID: id, ExerciseName: e.Exercise.Name, Kind: kind, Value: round1(v), Unit: l.s.unit,
						AchievedOn: w.StartedAt.In(l.s.loc).Format(time.DateOnly), WorkoutID: w.ID, SetID: st.ID, Reps: st.Reps, Load: &ld}
				}
			}
		}
	}
	return out
}

// changedRecords lists records that are new or moved, with the value before when there was one.
func changedRecords(before, after map[recordKey]Record) []Record {
	out := []Record{}
	for k, r := range after {
		was, ok := before[k]
		if ok && was.Value == r.Value {
			continue
		}
		if ok {
			r.Previous = &was.Value
		}
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b Record) int {
		return strings.Compare(a.ExerciseID+a.Kind, b.ExerciseID+b.Kind)
	})
	return out
}

// RecordList is get_personal_records' result.
type RecordList struct {
	Records []Record `json:"records"`
	Summary string   `json:"summary"`
}

func (r RecordList) summary() string { return r.Summary }

// Records returns the current records, for one exercise or all.
func (l *personLog) Records(exercise string) (Result, error) {
	var only string
	if exercise != "" {
		ex, err := l.resolve(exercise)
		if err != nil {
			return nil, err
		}
		only = ex.ID
	}
	out := RecordList{Records: []Record{}}
	for _, r := range changedRecords(nil, l.records()) {
		if only == "" || r.ExerciseID == only {
			out.Records = append(out.Records, r)
		}
	}
	out.Summary = fmt.Sprintf("%d personal records.", len(out.Records))
	return out, nil
}

// resolve finds the exercise a read names: an ID, an exact name, or the one close match.
func (l *personLog) resolve(exercise string) (*CatalogueExercise, error) {
	if ex := l.s.catalogue.ByID(exercise); ex != nil {
		return ex, nil
	}
	exact, candidates := l.s.catalogue.Match(exercise)
	switch {
	case exact != nil:
		return exact, nil
	case len(candidates) == 1:
		return candidates[0], nil
	case len(candidates) > 1:
		names := make([]string, len(candidates))
		for i, c := range candidates {
			names[i] = fmt.Sprintf("%s (%s)", c.Name, c.ID)
		}
		return nil, fail("exercise_ambiguous", "%q could mean %s. Ask the user which one, then retry with its ID", exercise, strings.Join(names, ", "))
	}
	return nil, invalid("exercise", "no exercise called %q; find one with search_exercises", exercise)
}

// --- Workouts ---

// Totals add up the workouts listed.
type Totals struct {
	Workouts int     `json:"workouts"`
	Sets     int     `json:"sets"`
	Volume   float64 `json:"volume"`
	Unit     string  `json:"unit"`
}

// WorkoutList is get_workouts' result.
type WorkoutList struct {
	From      string    `json:"from"`
	To        string    `json:"to"`
	Exercise  string    `json:"exercise,omitempty"`
	Workouts  []Workout `json:"workouts"`
	Totals    Totals    `json:"totals"`
	Truncated bool      `json:"truncated"`
	Summary   string    `json:"summary"`
}

func (r WorkoutList) summary() string { return r.Summary }

// days reads an inclusive range of days in the person's timezone, defaulting to the given span
// ending today.
func (l *personLog) days(from, to string, span int) (time.Time, time.Time, error) {
	today := l.s.now().In(l.s.loc).Format(time.DateOnly)
	if to == "" {
		to = today
	}
	end, err := time.ParseInLocation(time.DateOnly, to, l.s.loc)
	if err != nil {
		return time.Time{}, time.Time{}, invalid("to", "use YYYY-MM-DD")
	}
	start := end.AddDate(0, 0, -span)
	if from != "" {
		if start, err = time.ParseInLocation(time.DateOnly, from, l.s.loc); err != nil {
			return time.Time{}, time.Time{}, invalid("from", "use YYYY-MM-DD")
		}
	}
	if start.After(end) {
		return time.Time{}, time.Time{}, invalid("from", "must not be after to")
	}
	return start, end.AddDate(0, 0, 1), nil
}

// Workouts lists workouts between two days, newest first; with an exercise, only workouts with
// a confirmed entry of it, showing just those entries.
func (l *personLog) Workouts(from, to, exercise string, limit int) (Result, error) {
	start, end, err := l.days(from, to, 30)
	if err != nil {
		return nil, err
	}
	var only string
	if exercise != "" {
		ex, err := l.resolve(exercise)
		if err != nil {
			return nil, err
		}
		only = ex.ID
	}
	if limit <= 0 {
		limit = 10
	}
	limit = min(limit, 100)
	out := WorkoutList{From: start.Format(time.DateOnly), To: end.AddDate(0, 0, -1).Format(time.DateOnly), Exercise: only,
		Workouts: []Workout{}, Totals: Totals{Unit: l.s.unit}}
	for _, w := range l.inRange(start, end) {
		if only != "" {
			// Only the exercise's entries, so the estimates' pointers name positions among them.
			just := *w
			just.Exercises = slices.DeleteFunc(slices.Clone(w.Exercises), func(e *Entry) bool { return e.confirmedID() != only })
			if len(just.Exercises) == 0 {
				continue
			}
			w = &just
		}
		v := w.view()
		if len(out.Workouts) == limit {
			out.Truncated = true
			break
		}
		out.Workouts = append(out.Workouts, v)
		out.Totals.Workouts++
		for _, e := range v.Exercises {
			for _, st := range e.Sets {
				out.Totals.Sets++
				if !st.isWarmup() && st.Load != nil && st.Reps != nil {
					out.Totals.Volume += inUnit(*st.Load, l.s.unit) * float64(*st.Reps)
				}
			}
		}
	}
	out.Totals.Volume = round1(out.Totals.Volume)
	out.Summary = fmt.Sprintf("%d workouts from %s to %s: %d sets.", out.Totals.Workouts, out.From, out.To, out.Totals.Sets)
	return out, nil
}

// inRange returns the workouts started in [start, end), newest first.
func (l *personLog) inRange(start, end time.Time) []*Workout {
	var out []*Workout
	for _, w := range l.workouts {
		if !w.StartedAt.Before(start) && w.StartedAt.Before(end) {
			out = append(out, w)
		}
	}
	slices.SortFunc(out, func(a, b *Workout) int { return b.StartedAt.Compare(a.StartedAt) })
	return out
}

// --- Progress ---

var metricFormulas = map[string]string{
	"e1rm":    "best estimated one-rep max per session, Epley: load × (1 + reps / 30), from working sets of 1–12 reps",
	"top_set": "heaviest working set per session",
	"volume":  "total volume per session: sum of load × reps over working sets",
}

// Point is one session in a progress series.
type Point struct {
	Date      string  `json:"date"`
	WorkoutID string  `json:"workout_id"`
	Value     float64 `json:"value"`
	Reps      *int    `json:"reps,omitempty"`
}

// Progress is get_progress' result.
type Progress struct {
	ExerciseID   string  `json:"exercise_id"`
	ExerciseName string  `json:"exercise_name"`
	Metric       string  `json:"metric"`
	Formula      string  `json:"formula"`
	Unit         string  `json:"unit"`
	From         string  `json:"from"`
	To           string  `json:"to"`
	Points       []Point `json:"points"`
	Summary      string  `json:"summary"`
}

func (r Progress) summary() string { return r.Summary }

// Progress charts one metric for one exercise, a point per session, oldest first.
func (l *personLog) Progress(exercise, metric, from, to string) (Result, error) {
	formula, ok := metricFormulas[metric]
	if !ok {
		return nil, invalid("metric", "use e1rm, top_set or volume")
	}
	ex, err := l.resolve(exercise)
	if err != nil {
		return nil, err
	}
	start, end, err := l.days(from, to, 90)
	if err != nil {
		return nil, err
	}
	out := Progress{ExerciseID: ex.ID, ExerciseName: ex.Name, Metric: metric, Formula: formula, Unit: l.s.unit,
		From: start.Format(time.DateOnly), To: end.AddDate(0, 0, -1).Format(time.DateOnly), Points: []Point{}}
	sessions := l.inRange(start, end)
	slices.Reverse(sessions)
	for _, w := range sessions {
		if p, ok := l.point(w, ex.ID, metric); ok {
			out.Points = append(out.Points, p)
		}
	}
	out.Summary = fmt.Sprintf("%s %s: %d sessions.", ex.Name, metric, len(out.Points))
	if n := len(out.Points); n > 1 {
		out.Summary = fmt.Sprintf("%s %s: %d sessions, %s → %s %s.", ex.Name, metric, n,
			number(out.Points[0].Value), number(out.Points[n-1].Value), l.s.unit)
	}
	return out, nil
}

func (l *personLog) point(w *Workout, exerciseID, metric string) (Point, bool) {
	p := Point{Date: w.StartedAt.In(l.s.loc).Format(time.DateOnly), WorkoutID: w.ID}
	found := false
	for _, e := range w.Exercises {
		if e.confirmedID() != exerciseID {
			continue
		}
		for _, st := range e.Sets {
			if st.isWarmup() || st.Load == nil || st.Reps == nil {
				continue
			}
			load := inUnit(*st.Load, l.s.unit)
			switch metric {
			case "volume":
				p.Value += load * float64(*st.Reps)
				found = true
			case "top_set":
				if !found || load > p.Value {
					p.Value, p.Reps, found = load, st.Reps, true
				}
			case "e1rm":
				if v, ok := e1rm(load, *st.Reps); ok && (!found || v > p.Value) {
					p.Value, found = v, true
				}
			}
		}
	}
	p.Value = round1(p.Value)
	return p, found
}

// --- Search ---

// ExerciseMatch is an exercise search_exercises found.
type ExerciseMatch struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Equipment      []string `json:"equipment"`
	PrimaryMuscles []string `json:"primary_muscles"`
	IsCustom       bool     `json:"is_custom"`
	Score          float64  `json:"score"`
}

// SearchResult is search_exercises' result.
type SearchResult struct {
	Exercises []ExerciseMatch `json:"exercises"`
	Summary   string          `json:"-"`
}

func (r SearchResult) summary() string { return r.Summary }

// Search finds catalogue exercises by name or alias, optionally by equipment or muscle.
func (l *personLog) Search(query, equipment, muscle string, limit int) (Result, error) {
	if strings.TrimSpace(query) == "" {
		return nil, invalid("query", "search for an exercise name")
	}
	if limit <= 0 || limit > 25 {
		limit = 10
	}
	out := SearchResult{Exercises: []ExerciseMatch{}}
	var names []string
	for _, s := range l.s.catalogue.rank(query, 0.01) {
		ex := s.exercise
		if equipment != "" && !slices.Contains(ex.Equipment, equipment) || muscle != "" && !slices.Contains(ex.PrimaryMuscles, muscle) {
			continue
		}
		out.Exercises = append(out.Exercises, ExerciseMatch{ID: ex.ID, Name: ex.Name, Equipment: ex.Equipment,
			PrimaryMuscles: ex.PrimaryMuscles, Score: round1(s.score)})
		names = append(names, fmt.Sprintf("%s (%s)", ex.Name, ex.ID))
		if len(out.Exercises) == limit {
			break
		}
	}
	out.Summary = fmt.Sprintf("Found %d: %s.", len(names), strings.Join(names, ", "))
	if len(names) == 0 {
		out.Summary = "No exercises found; try another name."
	}
	return out, nil
}
