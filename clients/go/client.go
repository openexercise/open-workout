// Package openworkout is a thin Go client for an Open Workout server (the API core,
// api/core.yaml in openexercise/open-workout): net/http, the access token on every request, an
// Idempotency-Key on every POST, RFC 9457 problems as errors, and keyset paging over the workout
// list and the change feed. Workouts are Open Workout Schema v0 documents
// (https://openworkout.dev/schema/v0/workout.json).
//
// Methods marked "Lifterclub extension" call routes outside the core that Lifterclub, the
// reference implementation (LifterclubURL), serves; another server may not. Getting the token is
// the app's job: OAuth 2.1 with PKCE. Routes without a method here go through Do.
package openworkout

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// LifterclubURL is the origin of Lifterclub's API, the reference implementation.
const LifterclubURL = "https://api.lifterclub.com"

// Client calls the API as one user, with their token. The zero value is not usable: use New.
type Client struct {
	// BaseURL is the API's origin, without a trailing slash.
	BaseURL string
	// Token is the access token (lca_…) sent as a bearer token. TokenSource, when set, wins.
	Token string
	// TokenSource gives the current token before each request, for an app that refreshes.
	TokenSource func(ctx context.Context) (string, error)
	// HTTPClient sends the requests; http.DefaultClient when nil.
	HTTPClient *http.Client
	// UserAgent names the app in the User-Agent header, when set.
	UserAgent string
	// NewIdempotencyKey makes the Idempotency-Key for a POST whose context carries none
	// (WithIdempotencyKey). Random when nil.
	NewIdempotencyKey func() string
}

// New makes a client for the server at baseURL (LifterclubURL, say) with the token.
func New(baseURL, token string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token}
}

type idempotencyKey struct{}

// WithIdempotencyKey sets the Idempotency-Key the next POST sends, so a retry after a lost
// reply, even across restarts, returns the first result instead of writing twice. Without it
// each call gets a fresh key.
func WithIdempotencyKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, idempotencyKey{}, key)
}

// Error is a failed call: an RFC 9457 problem (Code is the API's error code, the same the
// MCP tools use) or an RFC 6750 bearer error (Code is invalid_token or insufficient_scope).
type Error struct {
	// Status is the HTTP status.
	Status int
	// Code is the problem's code, the bearer error, or "http_<status>" for anything else.
	Code   string
	Title  string
	Detail string
	// Field names the request field at fault, for invalid_input.
	Field string
	// Candidates are the exercises an ambiguous name could mean, for exercise_ambiguous.
	Candidates []Candidate
	// Warnings say why no workout was found in text, for nothing_to_log and parse_unsure.
	Warnings []Warning
	// RetryAfter is how long to wait when rate limited (CodeRateLimited); Detail says whether
	// the account's or the app's limit was hit. Zero otherwise.
	RetryAfter time.Duration
	// WWWAuthenticate is the challenge on a 401 or 403 bearer error.
	WWWAuthenticate string
}

// Error codes, as docs/api.md lists them.
const (
	CodeInvalidInput        = "invalid_input"
	CodeConsentRequired     = "consent_required"
	CodeNothingToLog        = "nothing_to_log"
	CodeParseUnsure         = "parse_unsure"
	CodeExerciseAmbiguous   = "exercise_ambiguous"
	CodeNotFound            = "not_found"
	CodeUndoUnavailable     = "undo_unavailable"
	CodeRateLimited         = "rate_limited"
	CodeIdempotencyConflict = "idempotency_conflict"
	CodeUnauthenticated     = "unauthenticated"
	CodeInternalError       = "internal_error"
	// CodeInvalidToken is the bearer error for a missing, expired or revoked token.
	CodeInvalidToken = "invalid_token"
	// CodeInsufficientScope is the bearer error for a token without the scope.
	CodeInsufficientScope = "insufficient_scope"
)

func (e *Error) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("openworkout: %s (%d)", e.Code, e.Status)
	}
	return fmt.Sprintf("openworkout: %s (%d): %s", e.Code, e.Status, e.Detail)
}

// HasCode reports whether err is an *Error with the code.
func HasCode(err error, code string) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}

// Do sends one request and decodes a JSON reply into out (which may be nil). query may be nil;
// body, when not nil, is sent as JSON. A POST carries an Idempotency-Key. A status outside
// 2xx is an *Error.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	res, err := c.send(ctx, method, path, query, body)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if out == nil {
		_, _ = io.Copy(io.Discard, res.Body)
		return nil
	}
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("openworkout: decode %s %s: %w", method, path, err)
	}
	return nil
}

// send does the HTTP work: a 2xx response is returned with its body open; anything else is
// read into an *Error.
func (c *Client) send(ctx context.Context, method, path string, query url.Values, body any) (*http.Response, error) {
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("openworkout: encode %s %s: %w", method, path, err)
		}
		payload = bytes.NewReader(raw)
	}
	target := c.BaseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, payload)
	if err != nil {
		return nil, fmt.Errorf("openworkout: %w", err)
	}
	token := c.Token
	if c.TokenSource != nil {
		if token, err = c.TokenSource(ctx); err != nil {
			return nil, fmt.Errorf("openworkout: token: %w", err)
		}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json, application/problem+json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	if method == http.MethodPost {
		req.Header.Set("Idempotency-Key", c.idempotencyKey(ctx))
	}
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openworkout: %w", err)
	}
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return res, nil
	}
	defer res.Body.Close()
	return nil, readError(res)
}

func (c *Client) idempotencyKey(ctx context.Context) string {
	if key, ok := ctx.Value(idempotencyKey{}).(string); ok && key != "" {
		return key
	}
	if c.NewIdempotencyKey != nil {
		return c.NewIdempotencyKey()
	}
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// readError turns a non-2xx response into an *Error: a problem document, a bearer error, or
// just the status.
func readError(res *http.Response) *Error {
	e := &Error{Status: res.StatusCode, Code: "http_" + strconv.Itoa(res.StatusCode), WWWAuthenticate: res.Header.Get("WWW-Authenticate")}
	if s, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && s > 0 {
		e.RetryAfter = time.Duration(s) * time.Second
	}
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	var doc struct {
		Title      string      `json:"title"`
		Detail     string      `json:"detail"`
		Code       string      `json:"code"`
		Field      string      `json:"field"`
		Candidates []Candidate `json:"candidates"`
		Warnings   []Warning   `json:"warnings"`
		// Bearer errors (RFC 6750).
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		e.Detail = strings.TrimSpace(string(raw))
		return e
	}
	switch {
	case doc.Code != "":
		e.Code, e.Title, e.Detail, e.Field, e.Candidates, e.Warnings = doc.Code, doc.Title, doc.Detail, doc.Field, doc.Candidates, doc.Warnings
	case doc.Error != "":
		e.Code, e.Detail = doc.Error, doc.ErrorDescription
	}
	return e
}

// pathID escapes one path segment.
func pathID(id string) string { return url.PathEscape(id) }
