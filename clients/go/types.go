package openworkout

import "time"

// Date is a day as YYYY-MM-DD in the user's timezone.
type Date = string

// Unit is a load unit.
type Unit string

const (
	Kg Unit = "kg"
	Lb Unit = "lb"
)

// Open Workout Schema v0 (https://openworkout.dev/schema/v0/workout.json).

// Workout is one workout: exercise entries with their sets, and where the record came from.
type Workout struct {
	Schema    string     `json:"$schema,omitempty"`
	ID        string     `json:"id,omitempty"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	// Date is the workout's local day (StartedAt in Timezone): file it under this day.
	Date Date `json:"date,omitempty"`
	// Timezone is the IANA zone the workout happened in.
	Timezone string `json:"timezone,omitempty"`
	// UpdatedAt is when the stored record last changed: the workout, an entry, a set or the
	// program label. Mirror by it.
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
	Title     string     `json:"title,omitempty"`
	Notes     string     `json:"notes,omitempty"`
	Source    Source     `json:"source"`
	// MergedSources are other apps' records of the same session, merged into this one.
	MergedSources []Source `json:"merged_sources,omitempty"`
	Exercises     []Entry  `json:"exercises"`
	// Program is the program day this workout was part of: a label only.
	Program *WorkoutProgram `json:"program,omitempty"`
	// Estimates are JSON pointers (RFC 6901) to values inferred or assumed rather than given.
	Estimates []string `json:"estimates,omitempty"`
}

// Source says where a workout came from.
type Source struct {
	// Kind is app, mcp, api or import.
	Kind string `json:"kind"`
	// Client is the client or app: a partner's slug, claude, strong, hevy…
	Client string `json:"client,omitempty"`
	// ExternalID is the workout's ID in the app it came from.
	ExternalID *string `json:"external_id,omitempty"`
}

// Entry is one exercise in a workout. ID is the entry's own ID, used to edit it.
type Entry struct {
	ID       string      `json:"id,omitempty"`
	Exercise ExerciseRef `json:"exercise"`
	// Text is what the user called the exercise.
	Text  string `json:"text,omitempty"`
	Notes string `json:"notes,omitempty"`
	Sets  []Set  `json:"sets"`
}

// ExerciseRef is the exercise an entry was matched to. Match catalogue and custom are
// confirmed; close and unmatched need confirmation and never count toward records.
type ExerciseRef struct {
	// ID is an Open Exercise ID (oec:<slug>) or a custom exercise ID; nil when nothing matched.
	ID    *string `json:"id"`
	Name  string  `json:"name"`
	Match string  `json:"match"`
}

// Set is one set. Type is working, warmup, backoff, amrap, failure or drop.
type Set struct {
	ID           string   `json:"id,omitempty"`
	Type         string   `json:"type,omitempty"`
	Reps         *int     `json:"reps,omitempty"`
	Load         *Load    `json:"load,omitempty"`
	RPE          *float64 `json:"rpe,omitempty"`
	DurationS    *int     `json:"duration_s,omitempty"`
	DistanceM    *float64 `json:"distance_m,omitempty"`
	Calories     *int     `json:"calories,omitempty"`
	HeartRateBPM *int     `json:"heart_rate_bpm,omitempty"`
}

// Load is a weight in the unit the user gave it.
type Load struct {
	Value float64 `json:"value"`
	Unit  Unit    `json:"unit"`
}

// WorkoutProgram is the program day a workout was part of. Listed false means the program was
// withdrawn, and says nothing more.
type WorkoutProgram struct {
	Listed  bool   `json:"listed"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Version int    `json:"version,omitempty"`
	Day     string `json:"day,omitempty"`
	DayName string `json:"day_name,omitempty"`
}

// Writes.

// WorkoutInput logs a workout (POST /v1/workouts).
type WorkoutInput struct {
	StartedAt *time.Time `json:"started_at,omitempty"`
	// Date is the day in the workout's timezone when StartedAt is not given; default today. The
	// start is then worked out, and listed in Workout.Estimates as /started_at.
	Date Date `json:"date,omitempty"`
	// Timezone is the IANA zone the workout happened in; default the person's.
	Timezone  string          `json:"timezone,omitempty"`
	Title     string          `json:"title,omitempty"`
	Notes     string          `json:"notes,omitempty"`
	Exercises []ExerciseInput `json:"exercises"`
	Program   *ProgramRef     `json:"program,omitempty"`
	// ExternalID is your app's own ID for the workout: a second POST with it replays the first
	// (Logged.Replayed) for ever, and one the person deleted here is not_found and stays deleted.
	ExternalID string `json:"external_id,omitempty"`
}

// ExerciseInput is one exercise to log, by name as the user said it or by ID when known.
type ExerciseInput struct {
	Name       string     `json:"name,omitempty"`
	ExerciseID string     `json:"exercise_id,omitempty"`
	Sets       []SetInput `json:"sets"`
	Notes      string     `json:"notes,omitempty"`
}

// SetInput is one set to log. Leave Load nil for bodyweight and Unit empty for the user's unit
// (then listed in Workout.Estimates as assumed).
type SetInput struct {
	Reps         *int     `json:"reps,omitempty"`
	Load         *float64 `json:"load,omitempty"`
	Unit         Unit     `json:"unit,omitempty"`
	RPE          *float64 `json:"rpe,omitempty"`
	DurationS    *int     `json:"duration_s,omitempty"`
	DistanceM    *float64 `json:"distance_m,omitempty"`
	Calories     *int     `json:"calories,omitempty"`
	HeartRateBPM *int     `json:"heart_rate_bpm,omitempty"`
	// Warmup sets don't count toward records.
	Warmup bool `json:"warmup,omitempty"`
	// Estimated marks values inferred (read from a photo, say) rather than stated.
	Estimated bool `json:"estimated,omitempty"`
}

// ProgramRef links a logged workout to a program day.
type ProgramRef struct {
	ID      string `json:"id"`
	Version int    `json:"version,omitempty"`
	Day     string `json:"day,omitempty"`
}

// TextRequest logs or parses the user's own words.
type TextRequest struct {
	Text string `json:"text"`
	Date Date   `json:"date,omitempty"`
}

// WorkoutPatch changes a workout. EndedAt finishes a live session.
type WorkoutPatch struct {
	Title   *string    `json:"title,omitempty"`
	Date    Date       `json:"date,omitempty"`
	EndedAt *time.Time `json:"ended_at,omitempty"`
	Notes   *string    `json:"notes,omitempty"`
}

// AddSetsRequest adds sets to a workout (POST /v1/workouts/{id}/sets; the ID may be "current").
type AddSetsRequest struct {
	// Exercise is the name as the user said it; ExerciseID skips matching.
	Exercise   string     `json:"exercise,omitempty"`
	ExerciseID string     `json:"exercise_id,omitempty"`
	Sets       []SetInput `json:"sets"`
}

// EntryPatch confirms an entry's exercise (from Confirmation.Candidates or SearchExercises) or
// changes its notes.
type EntryPatch struct {
	ExerciseID string  `json:"exercise_id,omitempty"`
	Notes      *string `json:"notes,omitempty"`
}

// SetPatch changes a set. RemoveLoad makes it bodyweight.
type SetPatch struct {
	Reps         *int     `json:"reps,omitempty"`
	Load         *float64 `json:"load,omitempty"`
	Unit         Unit     `json:"unit,omitempty"`
	RemoveLoad   bool     `json:"remove_load,omitempty"`
	RPE          *float64 `json:"rpe,omitempty"`
	DurationS    *int     `json:"duration_s,omitempty"`
	DistanceM    *float64 `json:"distance_m,omitempty"`
	Calories     *int     `json:"calories,omitempty"`
	HeartRateBPM *int     `json:"heart_rate_bpm,omitempty"`
	Warmup       *bool    `json:"warmup,omitempty"`
}

// Logged is the result of a write: the workout after it, and what changed.
type Logged struct {
	Workout Workout `json:"workout"`
	// Summary is one line to show the user.
	Summary    string   `json:"summary"`
	NewRecords []Record `json:"new_records"`
	// NeedsConfirmation lists entries whose exercise to confirm with the user, then fix with
	// UpdateEntry.
	NeedsConfirmation []Confirmation `json:"needs_confirmation"`
	// Replayed is true when the response repeats an earlier request with the same
	// Idempotency-Key or external_id; nothing new was stored.
	Replayed bool `json:"replayed,omitempty"`
	// ParseMethod is how text was read (llm or fallback), for a write made from text.
	ParseMethod string    `json:"parse_method,omitempty"`
	Warnings    []Warning `json:"warnings,omitempty"`
}

// Record is a personal record a write changed.
type Record struct {
	ExerciseID   string  `json:"exercise_id"`
	ExerciseName string  `json:"exercise_name"`
	Kind         string  `json:"kind"`
	Value        float64 `json:"value"`
	Unit         Unit    `json:"unit"`
	// Previous is absent for a first record.
	Previous *float64 `json:"previous,omitempty"`
	SetID    string   `json:"set_id"`
	// Removed means no set qualifies any more; Value is 0.
	Removed bool `json:"removed,omitempty"`
}

// Confirmation is an exercise to confirm with the user.
type Confirmation struct {
	EntryID    string      `json:"entry_id"`
	Text       string      `json:"text"`
	Status     string      `json:"status"`
	Candidates []Candidate `json:"candidates"`
}

// Candidate is an exercise a name could mean.
type Candidate struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	MeasurementType string  `json:"measurement_type"`
	IsCustom        bool    `json:"is_custom"`
	Score           float64 `json:"score"`
}

// Warning says why text couldn't be read in full.
type Warning struct {
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

// Deleted is the result of a delete, with the token that undoes it for 10 minutes.
type Deleted struct {
	// Kind is workout, exercise or set.
	Kind           string    `json:"kind"`
	ID             string    `json:"id"`
	UndoToken      string    `json:"undo_token"`
	UndoExpiresAt  time.Time `json:"undo_expires_at"`
	RecordsChanged []Record  `json:"records_changed"`
	Summary        string    `json:"summary"`
}

// Draft is parsed text, not saved (POST /v1/parse).
type Draft struct {
	Text string `json:"text"`
	// Method is llm, fallback or none.
	Method    string          `json:"method"`
	Date      Date            `json:"date,omitempty"`
	DateText  string          `json:"date_text,omitempty"`
	Exercises []DraftExercise `json:"exercises"`
	Warnings  []Warning       `json:"warnings"`
}

// DraftExercise is one exercise read from text, with its match.
type DraftExercise struct {
	Text  string     `json:"text"`
	Sets  []DraftSet `json:"sets"`
	Notes string     `json:"notes,omitempty"`
	Match Match      `json:"match"`
}

// DraftSet is one set read from text; nil means the text didn't say.
type DraftSet struct {
	Reps      *int     `json:"reps"`
	Load      *float64 `json:"load"`
	Unit      *Unit    `json:"unit"`
	RPE       *float64 `json:"rpe"`
	DurationS *int     `json:"duration_s"`
	DistanceM *float64 `json:"distance_m"`
	Warmup    bool     `json:"warmup"`
	// UnitAssumed means the user's default unit was applied.
	UnitAssumed bool `json:"unit_assumed"`
}

// Match is how a phrase matched an exercise.
type Match struct {
	Phrase     string      `json:"phrase"`
	Status     string      `json:"status"`
	Exercise   *Candidate  `json:"exercise"`
	Confidence float64     `json:"confidence"`
	Method     string      `json:"method"`
	Candidates []Candidate `json:"candidates"`
}

// Reads.

// Me is the signed-in user (GET /v1/me).
type Me struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	// UnitSystem is metric or imperial.
	UnitSystem string `json:"unit_system"`
	Timezone   string `json:"timezone"`
	// CalmMode hides records, totals and progress numbers: don't volunteer them.
	CalmMode  bool `json:"calm_mode"`
	Onboarded bool `json:"onboarded"`
	// HealthConsent is in force; writes need it.
	HealthConsent bool `json:"health_consent"`
}

// WorkoutList is a page of workouts, or a page of the change feed.
type WorkoutList struct {
	From     Date      `json:"from,omitempty"`
	To       Date      `json:"to,omitempty"`
	Exercise string    `json:"exercise,omitempty"`
	Workouts []Workout `json:"workouts"`
	// UpdatedSince and Deleted belong to the change feed.
	UpdatedSince string      `json:"updated_since,omitempty"`
	Deleted      []Tombstone `json:"deleted,omitempty"`
	Totals       Totals      `json:"totals"`
	// Truncated means more follow; pass NextCursor with the same filters.
	Truncated  bool   `json:"truncated"`
	NextCursor string `json:"next_cursor,omitempty"`
	Summary    string `json:"summary"`
}

// Tombstone is a deleted workout in the change feed: enough to remove a mirror's copy.
type Tombstone struct {
	ID        string    `json:"id"`
	DeletedAt time.Time `json:"deleted_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// MergedInto is the workout it was merged into, when another app's record absorbed it.
	MergedInto string `json:"merged_into,omitempty"`
}

// Totals add up a page's working sets.
type Totals struct {
	Workouts int     `json:"workouts"`
	Sets     int     `json:"sets"`
	Volume   float64 `json:"volume"`
	Unit     Unit    `json:"unit"`
}

// RecordList is the user's current personal records.
type RecordList struct {
	Records []CurrentRecord `json:"records"`
	Summary string          `json:"summary"`
}

// CurrentRecord is one personal record: heaviest_load, best_e1rm or best_set_volume.
type CurrentRecord struct {
	ExerciseID   string  `json:"exercise_id"`
	ExerciseName string  `json:"exercise_name"`
	Kind         string  `json:"kind"`
	Value        float64 `json:"value"`
	Unit         Unit    `json:"unit"`
	AchievedOn   Date    `json:"achieved_on"`
	WorkoutID    string  `json:"workout_id"`
	SetID        string  `json:"set_id"`
	Reps         *int    `json:"reps,omitempty"`
	Load         *Load   `json:"load,omitempty"`
}

// Progress is one metric for one exercise over time: one point per session.
type Progress struct {
	ExerciseID   string `json:"exercise_id"`
	ExerciseName string `json:"exercise_name"`
	// Metric is e1rm, top_set or volume; Formula says how each value is computed.
	Metric  string   `json:"metric"`
	Formula string   `json:"formula"`
	Unit    Unit     `json:"unit"`
	From    Date     `json:"from"`
	To      Date     `json:"to"`
	Points  []Point  `json:"points"`
	Best    *Point   `json:"best,omitempty"`
	Change  *float64 `json:"change,omitempty"`
	Summary string   `json:"summary"`
}

// Point is one session's value.
type Point struct {
	Date      Date    `json:"date"`
	WorkoutID string  `json:"workout_id"`
	Value     float64 `json:"value"`
	Reps      *int    `json:"reps,omitempty"`
}

// ExerciseMatch is an exercise from search, or one the user made.
type ExerciseMatch struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	MeasurementType string   `json:"measurement_type"`
	Equipment       []string `json:"equipment"`
	PrimaryMuscles  []string `json:"primary_muscles"`
	IsCustom        bool     `json:"is_custom"`
	Score           float64  `json:"score"`
}

// ExerciseRequest makes one of the user's own exercises.
type ExerciseRequest struct {
	Name            string `json:"name"`
	MeasurementType string `json:"measurement_type,omitempty"`
}

// LastSets is what the user did the last time they did an exercise.
type LastSets struct {
	ExerciseID   string `json:"exercise_id"`
	ExerciseName string `json:"exercise_name"`
	// WorkoutID is nil when the exercise was never logged.
	WorkoutID *string    `json:"workout_id"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	Sets      []Set      `json:"sets"`
	Summary   string     `json:"summary"`
}

// Profile is the user's training profile.
type Profile struct {
	DisplayName  string   `json:"display_name"`
	Units        Unit     `json:"units"`
	Timezone     string   `json:"timezone"`
	Goals        string   `json:"goals"`
	Equipment    []string `json:"equipment"`
	Limitations  string   `json:"limitations"`
	TrainingDays []string `json:"training_days"`
	// CalmMode hides records, totals and progress numbers: don't volunteer them.
	CalmMode               bool   `json:"calm_mode"`
	ProgramSuggestions     bool   `json:"program_suggestions"`
	ProgramCounts          bool   `json:"program_counts"`
	ProgramCountsAvailable bool   `json:"program_counts_available"`
	HealthConsent          bool   `json:"health_consent"`
	Summary                string `json:"summary"`
}

// ProfilePatch changes profile fields; nil fields stay as they are.
type ProfilePatch struct {
	DisplayName        *string   `json:"display_name,omitempty"`
	Units              Unit      `json:"units,omitempty"`
	Timezone           *string   `json:"timezone,omitempty"`
	ProgramCounts      *bool     `json:"program_counts,omitempty"`
	ProgramSuggestions *bool     `json:"program_suggestions,omitempty"`
	CalmMode           *bool     `json:"calm_mode,omitempty"`
	Goals              *string   `json:"goals,omitempty"`
	Equipment          *[]string `json:"equipment,omitempty"`
	Limitations        *string   `json:"limitations,omitempty"`
	TrainingDays       *[]string `json:"training_days,omitempty"`
}

// Webhook events.
const (
	EventWorkoutCreated = "workout.created"
	EventWorkoutUpdated = "workout.updated"
	EventWorkoutDeleted = "workout.deleted"
)

// WebhookInput subscribes to changes: an https URL on a public host and the events wanted.
type WebhookInput struct {
	URL    string   `json:"url"`
	Events []string `json:"events"`
}

// Webhook is a subscription. Secret is set only in the creation response.
type Webhook struct {
	ID        string     `json:"id"`
	URL       string     `json:"url"`
	Events    []string   `json:"events"`
	CreatedAt time.Time  `json:"created_at"`
	PausedAt  *time.Time `json:"paused_at,omitempty"`
	// LastDeliveredAt and LastStatus describe the last delivery; LastStatus 0 means the
	// endpoint couldn't be reached.
	LastDeliveredAt     *time.Time `json:"last_delivered_at,omitempty"`
	LastStatus          *int       `json:"last_status,omitempty"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	Secret              string     `json:"secret,omitempty"`
}

// Delivery is the body of a webhook POST: identifiers only, never the workout's content.
type Delivery struct {
	Event          string    `json:"event"`
	WorkoutID      string    `json:"workout_id"`
	OccurredAt     time.Time `json:"occurred_at"`
	SubscriptionID string    `json:"subscription_id"`
	DeliveryID     string    `json:"delivery_id"`
}
