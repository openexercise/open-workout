package openworkout

import (
	"context"
	"iter"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Me is the signed-in user (profile:read). Lifterclub extension.
func (c *Client) Me(ctx context.Context) (*Me, error) {
	var out Me
	return &out, c.Do(ctx, http.MethodGet, "/v1/me", nil, nil, &out)
}

// ListWorkoutsOptions filter GET /v1/workouts. Zero values are left out.
type ListWorkoutsOptions struct {
	// From and To bound the days listed, inclusive, in the user's timezone. Default: the 30
	// days up to today.
	From, To Date
	// Exercise keeps only workouts with this exercise (name or ID), showing just its entries.
	Exercise string
	// Limit is workouts per page, 1 to 100; default 20.
	Limit int
	// Cursor is NextCursor from the previous page.
	Cursor string
}

func (o ListWorkoutsOptions) query() url.Values {
	q := url.Values{}
	setIf(q, "from", o.From)
	setIf(q, "to", o.To)
	setIf(q, "exercise", o.Exercise)
	if o.Limit > 0 {
		q.Set("limit", strconv.Itoa(o.Limit))
	}
	setIf(q, "cursor", o.Cursor)
	return q
}

// ListWorkouts is one page of workouts, newest first.
func (c *Client) ListWorkouts(ctx context.Context, opts ListWorkoutsOptions) (*WorkoutList, error) {
	var out WorkoutList
	return &out, c.Do(ctx, http.MethodGet, "/v1/workouts", opts.query(), nil, &out)
}

// Workouts is every workout matching opts, newest first, following NextCursor page by page.
// A failed page ends the sequence with its error.
func (c *Client) Workouts(ctx context.Context, opts ListWorkoutsOptions) iter.Seq2[Workout, error] {
	return func(yield func(Workout, error) bool) {
		for {
			page, err := c.ListWorkouts(ctx, opts)
			if err != nil {
				yield(Workout{}, err)
				return
			}
			for _, w := range page.Workouts {
				if !yield(w, nil) {
					return
				}
			}
			if !page.Truncated || page.NextCursor == "" {
				return
			}
			opts.Cursor = page.NextCursor
		}
	}
}

// ChangesOptions select the change feed: every workout whose stored record changed at or after
// UpdatedSince, oldest change first, and with IncludeDeleted the workouts deleted since then as
// tombstones. The feed ignores day and exercise filters.
type ChangesOptions struct {
	UpdatedSince   time.Time
	IncludeDeleted bool
	Limit          int
	Cursor         string
}

func (o ChangesOptions) query() url.Values {
	q := url.Values{"updated_since": {o.UpdatedSince.UTC().Format(time.RFC3339Nano)}}
	if o.IncludeDeleted {
		q.Set("include_deleted", "true")
	}
	if o.Limit > 0 {
		q.Set("limit", strconv.Itoa(o.Limit))
	}
	setIf(q, "cursor", o.Cursor)
	return q
}

// ListChanges is one page of the change feed.
func (c *Client) ListChanges(ctx context.Context, opts ChangesOptions) (*WorkoutList, error) {
	var out WorkoutList
	return &out, c.Do(ctx, http.MethodGet, "/v1/workouts", opts.query(), nil, &out)
}

// Change is one entry in the change feed: a workout as it is now, or a tombstone for one
// deleted. Exactly one is set.
type Change struct {
	Workout *Workout
	Deleted *Tombstone
}

// UpdatedAt is when the change happened: the next UpdatedSince to ask with, once stored.
func (ch Change) UpdatedAt() time.Time {
	if ch.Deleted != nil {
		return ch.Deleted.UpdatedAt
	}
	if ch.Workout != nil && ch.Workout.UpdatedAt != nil {
		return *ch.Workout.UpdatedAt
	}
	return time.Time{}
}

// Changes is the whole change feed from opts, page by page: each page's workouts, then its
// tombstones. Keep the latest UpdatedAt seen and pass it back as the next UpdatedSince.
func (c *Client) Changes(ctx context.Context, opts ChangesOptions) iter.Seq2[Change, error] {
	return func(yield func(Change, error) bool) {
		for {
			page, err := c.ListChanges(ctx, opts)
			if err != nil {
				yield(Change{}, err)
				return
			}
			for i := range page.Workouts {
				if !yield(Change{Workout: &page.Workouts[i]}, nil) {
					return
				}
			}
			for i := range page.Deleted {
				if !yield(Change{Deleted: &page.Deleted[i]}, nil) {
					return
				}
			}
			if !page.Truncated || page.NextCursor == "" {
				return
			}
			opts.Cursor = page.NextCursor
		}
	}
}

// GetWorkout is one workout.
func (c *Client) GetWorkout(ctx context.Context, id string) (*Workout, error) {
	var out Workout
	return &out, c.Do(ctx, http.MethodGet, "/v1/workouts/"+pathID(id), nil, nil, &out)
}

// LogWorkout logs a workout (workouts:write; needs the user's health-data consent).
func (c *Client) LogWorkout(ctx context.Context, in WorkoutInput) (*Logged, error) {
	var out Logged
	return &out, c.Do(ctx, http.MethodPost, "/v1/workouts", nil, in, &out)
}

// LogText parses the user's words and logs them in one step. A sentence the API can't read
// reliably is CodeParseUnsure, with nothing saved: send structure to LogWorkout instead.
// Lifterclub extension.
func (c *Client) LogText(ctx context.Context, in TextRequest) (*Logged, error) {
	var out Logged
	return &out, c.Do(ctx, http.MethodPost, "/v1/workouts/from-text", nil, in, &out)
}

// Parse reads text into a draft without saving, to show the user first. Lifterclub extension.
func (c *Client) Parse(ctx context.Context, in TextRequest) (*Draft, error) {
	var out Draft
	return &out, c.Do(ctx, http.MethodPost, "/v1/parse", nil, in, &out)
}

// UpdateWorkout changes a workout's title, day, notes, or finishes it.
func (c *Client) UpdateWorkout(ctx context.Context, id string, patch WorkoutPatch) (*Logged, error) {
	var out Logged
	return &out, c.Do(ctx, http.MethodPatch, "/v1/workouts/"+pathID(id), nil, patch, &out)
}

// DeleteWorkout deletes a workout; Undo restores it within 10 minutes.
func (c *Client) DeleteWorkout(ctx context.Context, id string) (*Deleted, error) {
	var out Deleted
	return &out, c.Do(ctx, http.MethodDelete, "/v1/workouts/"+pathID(id), nil, nil, &out)
}

// Undo restores what a delete removed, with the same IDs.
func (c *Client) Undo(ctx context.Context, undoToken string) (*Logged, error) {
	var out Logged
	return &out, c.Do(ctx, http.MethodPost, "/v1/undo", nil, map[string]string{"undo_token": undoToken}, &out)
}

// AddSets adds sets to a workout. WorkoutCurrent adds to the workout in progress (started in
// the last 4 hours), or starts one.
func (c *Client) AddSets(ctx context.Context, workoutID string, in AddSetsRequest) (*Logged, error) {
	var out Logged
	return &out, c.Do(ctx, http.MethodPost, "/v1/workouts/"+pathID(workoutID)+"/sets", nil, in, &out)
}

// WorkoutCurrent names the workout in progress, for AddSets.
const WorkoutCurrent = "current"

// AddEntrySets adds sets to an entry.
func (c *Client) AddEntrySets(ctx context.Context, entryID string, sets []SetInput) (*Logged, error) {
	var out Logged
	return &out, c.Do(ctx, http.MethodPost, "/v1/entries/"+pathID(entryID)+"/sets", nil, map[string][]SetInput{"sets": sets}, &out)
}

// UpdateEntry confirms an entry's exercise or changes its notes.
func (c *Client) UpdateEntry(ctx context.Context, entryID string, patch EntryPatch) (*Logged, error) {
	var out Logged
	return &out, c.Do(ctx, http.MethodPatch, "/v1/entries/"+pathID(entryID), nil, patch, &out)
}

// DeleteEntry deletes an entry and its sets.
func (c *Client) DeleteEntry(ctx context.Context, entryID string) (*Deleted, error) {
	var out Deleted
	return &out, c.Do(ctx, http.MethodDelete, "/v1/entries/"+pathID(entryID), nil, nil, &out)
}

// UpdateSet changes a set.
func (c *Client) UpdateSet(ctx context.Context, setID string, patch SetPatch) (*Logged, error) {
	var out Logged
	return &out, c.Do(ctx, http.MethodPatch, "/v1/sets/"+pathID(setID), nil, patch, &out)
}

// DeleteSet deletes a set.
func (c *Client) DeleteSet(ctx context.Context, setID string) (*Deleted, error) {
	var out Deleted
	return &out, c.Do(ctx, http.MethodDelete, "/v1/sets/"+pathID(setID), nil, nil, &out)
}

func setIf(q url.Values, key, value string) {
	if value != "" {
		q.Set(key, value)
	}
}
