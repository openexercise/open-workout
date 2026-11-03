package lifterclub

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// Records is the user's current personal records, for one exercise (name or ID) or all.
func (c *Client) Records(ctx context.Context, exercise string) (*RecordList, error) {
	q := url.Values{}
	setIf(q, "exercise", exercise)
	var out RecordList
	return &out, c.Do(ctx, http.MethodGet, "/v1/records", q, nil, &out)
}

// ProgressOptions select one metric for one exercise over a range of days.
type ProgressOptions struct {
	// Exercise is a name or ID.
	Exercise string
	// Metric is e1rm, top_set or volume.
	Metric string
	// From and To bound the days; default the 90 days up to today.
	From, To Date
}

// Progress is one point per session for the metric.
func (c *Client) Progress(ctx context.Context, opts ProgressOptions) (*Progress, error) {
	q := url.Values{"exercise": {opts.Exercise}, "metric": {opts.Metric}}
	setIf(q, "from", opts.From)
	setIf(q, "to", opts.To)
	var out Progress
	return &out, c.Do(ctx, http.MethodGet, "/v1/progress", q, nil, &out)
}

// SearchOptions filter exercise search.
type SearchOptions struct {
	// Query is a name or shorthand, e.g. "rdl".
	Query     string
	Equipment string
	Muscle    string
	// Limit is 1 to 25; default 10.
	Limit int
}

// SearchExercises searches the catalogue and the user's own exercises, best matches first.
func (c *Client) SearchExercises(ctx context.Context, opts SearchOptions) ([]ExerciseMatch, error) {
	q := url.Values{"query": {opts.Query}}
	setIf(q, "equipment", opts.Equipment)
	setIf(q, "muscle", opts.Muscle)
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	var out struct {
		Exercises []ExerciseMatch `json:"exercises"`
	}
	return out.Exercises, c.Do(ctx, http.MethodGet, "/v1/exercises", q, nil, &out)
}

// CreateExercise makes one of the user's own exercises, or returns their existing one by that
// name.
func (c *Client) CreateExercise(ctx context.Context, in ExerciseRequest) (*ExerciseMatch, error) {
	var out ExerciseMatch
	return &out, c.Do(ctx, http.MethodPost, "/v1/exercises", nil, in, &out)
}

// LastSets is what the user did the last time they did the exercise.
func (c *Client) LastSets(ctx context.Context, exerciseID string) (*LastSets, error) {
	var out LastSets
	return &out, c.Do(ctx, http.MethodGet, "/v1/exercises/"+pathID(exerciseID)+"/last-sets", nil, nil, &out)
}

// Profile is the user's training profile (profile:read).
func (c *Client) Profile(ctx context.Context) (*Profile, error) {
	var out Profile
	return &out, c.Do(ctx, http.MethodGet, "/v1/profile", nil, nil, &out)
}

// UpdateProfile changes the fields given (profile:write). Goals, equipment, limitations and
// training days need the user's health-data consent.
func (c *Client) UpdateProfile(ctx context.Context, patch ProfilePatch) (*Profile, error) {
	var out Profile
	return &out, c.Do(ctx, http.MethodPatch, "/v1/profile", nil, patch, &out)
}

// Export streams the whole log as a file: format is "json" (Open Workout Schema workouts and
// routines, plus the profile with profile:read) or "csv" (one row per set). Close the reader.
// Limited to 10 an hour per account. If the API fails part way the connection is cut, so a
// read error means the file is incomplete.
func (c *Client) Export(ctx context.Context, format string) (io.ReadCloser, error) {
	q := url.Values{}
	setIf(q, "format", format)
	res, err := c.send(ctx, http.MethodGet, "/v1/export", q, nil)
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}
