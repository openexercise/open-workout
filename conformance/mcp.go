package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	openworkout "github.com/openexercise/open-workout"
	"github.com/openexercise/open-workout/internal/ows"
)

// MCPChecks are the MCP script's steps, in the order they run.
var MCPChecks = []string{
	"discovery", "tools", "log", "replay", "needs-confirmation", "read", "errors", "read-only", "delete",
}

// profileKey is the _meta key naming the profile a tool belongs to.
const profileKey = "dev.openworkout/profile"

// MCPConfig says which MCP server to check and as whom.
type MCPConfig struct {
	// URL is the Streamable HTTP endpoint, such as https://mcp.lifterclub.com.
	URL string
	// Token is a person's credential an assistant would use, able to log workouts.
	Token string
	// ReadOnlyToken is a read-only credential for the same person. Without one, read-only is
	// skipped and the server can't pass.
	ReadOnlyToken string
	// HTTPClient sends the requests; http.DefaultClient when nil.
	HTTPClient *http.Client
}

// MCPReport is every MCP check's outcome, in order.
type MCPReport struct {
	URL     string   `json:"url"`
	Results []Result `json:"results"`
}

// Passed reports whether every check passed.
func (r MCPReport) Passed() bool {
	for _, res := range r.Results {
		if res.Status != Pass {
			return false
		}
	}
	return len(r.Results) == len(MCPChecks)
}

// RunMCP checks an MCP server against the profile's core (mcp/tools.json) as one person: it
// answers 401 with discovery, lists every core tool with the profile's input schema and
// annotations, logs a workout with an exercise no catalogue knows (which must come back in
// needs_confirmation), replays the idempotency_key, reads the workout back, returns tool errors
// that start with a profile code, keeps a read-only credential to read-only tools, and deletes the
// workout. The workout is deleted at the end, also when a check fails part way.
func RunMCP(ctx context.Context, cfg MCPConfig) (MCPReport, error) {
	endpoint, err := url.Parse(cfg.URL)
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" {
		return MCPReport{}, fmt.Errorf("MCP URL %q: want an http or https URL", cfg.URL)
	}
	if cfg.Token == "" {
		return MCPReport{}, errors.New("no token: the script logs a workout, so it needs a credential that can write")
	}
	v, err := ows.NewValidatorFS(openworkout.Schemas)
	if err != nil {
		return MCPReport{}, err
	}
	var profile struct {
		Instructions string      `json:"instructions"`
		ErrorCodes   []errorCode `json:"error_codes"`
		Tools        []*mcp.Tool `json:"tools"`
	}
	if err := json.Unmarshal(openworkout.Profile, &profile); err != nil {
		return MCPReport{}, fmt.Errorf("read the profile: %w", err)
	}
	m := &mcpRunner{cfg: cfg, v: v, run: random(), codes: map[string]bool{}}
	if m.client = cfg.HTTPClient; m.client == nil {
		m.client = http.DefaultClient
	}
	for _, c := range profile.ErrorCodes {
		m.codes[c.Code] = true
	}
	for _, t := range profile.Tools {
		if t.Meta[profileKey] == "core" {
			m.core = append(m.core, t)
		}
	}
	report := MCPReport{URL: endpoint.String()}
	steps := []func(context.Context) error{
		m.discovery, m.tools, m.log, m.replay, m.needsConfirmation, m.read, m.errors, m.readOnly, m.delete,
	}
	for i, step := range steps {
		res := Result{Name: MCPChecks[i], Status: Pass}
		if why := m.blocked(MCPChecks[i]); why != "" {
			res.Status, res.Detail = Skip, why
		} else if err := step(ctx); err != nil {
			res.Status, res.Detail = Fail, err.Error()
		}
		report.Results = append(report.Results, res)
	}
	m.cleanUp(ctx)
	if m.session != nil {
		m.session.Close()
	}
	return report, nil
}

type errorCode struct {
	Code string `json:"code"`
}

type mcpRunner struct {
	cfg     MCPConfig
	client  *http.Client
	v       *ows.Validator
	run     string
	codes   map[string]bool
	core    []*mcp.Tool
	session *mcp.ClientSession

	args      map[string]any // log_workout's arguments
	logged    map[string]any // its structured result
	id        string         // the workout's ID
	unmatched string         // the entry ID of the exercise no catalogue knows
	deleted   bool
}

func (m *mcpRunner) blocked(check string) string {
	switch check {
	case "discovery", "tools":
		return ""
	case "errors":
		if m.session == nil {
			return "needs a connection, which tools couldn't make"
		}
		return ""
	case "read-only":
		if m.cfg.ReadOnlyToken == "" {
			return "no read-only token: give one (--read-token or OWS_READ_TOKEN) for the same person"
		}
		return ""
	case "log":
		if m.session == nil {
			return "needs a connection, which tools couldn't make"
		}
		return ""
	}
	if m.id == "" {
		return "needs a workout, which log didn't store"
	}
	return ""
}

// discovery: without a credential, the endpoint answers 401 with a Bearer challenge naming its
// protected resource metadata (RFC 9728), which is how an MCP client finds where to sign in.
func (m *mcpRunner) discovery(ctx context.Context) error {
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"ows-conformance","version":"0"}}}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.cfg.URL, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	res, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("POST without a credential: %w", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		return fmt.Errorf("POST without a credential: status %d, want 401", res.StatusCode)
	}
	challenge := res.Header.Get("WWW-Authenticate")
	if !strings.HasPrefix(strings.ToLower(challenge), "bearer") || !strings.Contains(challenge, "resource_metadata=") {
		return fmt.Errorf("WWW-Authenticate %q: want a Bearer challenge with resource_metadata", challenge)
	}
	return nil
}

// connect opens a session with a credential.
func (m *mcpRunner) connect(ctx context.Context, token string) (*mcp.ClientSession, error) {
	base := m.client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	hc := *m.client
	hc.Transport = bearer{token: token, next: base}
	transport := &mcp.StreamableClientTransport{Endpoint: m.cfg.URL, HTTPClient: &hc}
	return mcp.NewClient(&mcp.Implementation{Name: "ows-conformance", Version: "0"}, nil).Connect(ctx, transport, nil)
}

type bearer struct {
	token string
	next  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.next.RoundTrip(r)
}

// tools: the server lists every core tool of the profile with the same input schema and
// annotations, marked core in its _meta. Descriptions are the server's own words.
func (m *mcpRunner) tools(ctx context.Context) error {
	s, err := m.connect(ctx, m.cfg.Token)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	m.session = s
	listed, err := listAll(ctx, s)
	if err != nil {
		return err
	}
	var problems []string
	for _, want := range m.core {
		got, ok := listed[want.Name]
		if !ok {
			problems = append(problems, want.Name+" isn't listed")
			continue
		}
		if !sameJSON(got.InputSchema, want.InputSchema) {
			problems = append(problems, want.Name+"'s input schema differs from the profile's")
		}
		if !sameJSON(got.Annotations, want.Annotations) {
			problems = append(problems, want.Name+"'s annotations differ from the profile's")
		}
		if got.Meta[profileKey] != "core" {
			problems = append(problems, fmt.Sprintf("%s's _meta %q is %v, want core", want.Name, profileKey, got.Meta[profileKey]))
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func listAll(ctx context.Context, s *mcp.ClientSession) (map[string]*mcp.Tool, error) {
	out := map[string]*mcp.Tool{}
	for t, err := range s.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("tools/list: %w", err)
		}
		out[t.Name] = t
	}
	return out, nil
}

func sameJSON(a, b any) bool {
	ra, err1 := json.Marshal(a)
	rb, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		return false
	}
	var x, y any
	_ = json.Unmarshal(ra, &x)
	_ = json.Unmarshal(rb, &y)
	return reflect.DeepEqual(x, y)
}

// log: log_workout stores a workout and returns, as the profile says, the summary to echo, then
// the result as JSON with the stored workout, a valid OWS document.
func (m *mcpRunner) log(ctx context.Context) error {
	m.args = map[string]any{
		"title":           "ows conformance " + m.run,
		"notes":           "Written by ows conformance mcp and deleted at the end of its run.",
		"idempotency_key": "ows-conformance-" + m.run,
		"exercises": []any{
			map[string]any{"name": "Zzqx Flurbo Conformance Lift", "sets": []any{
				map[string]any{"reps": 5, "load": 20, "unit": "kg"},
				map[string]any{"reps": 5, "load": 20, "unit": "kg"},
			}},
		},
	}
	out, err := m.call(ctx, m.session, "log_workout", m.args)
	if err != nil {
		return err
	}
	w, err := m.workout(out)
	if err != nil {
		return err
	}
	m.logged, m.id = out, w.ID
	if len(w.Exercises) > 0 {
		m.unmatched = w.Exercises[0].ID
	}
	return nil
}

type loggedWorkout struct {
	ID        string `json:"id"`
	Exercises []struct {
		ID string `json:"id"`
	} `json:"exercises"`
}

// workout reads and validates the workout in a write's result.
func (m *mcpRunner) workout(out map[string]any) (loggedWorkout, error) {
	raw, err := json.Marshal(out["workout"])
	if err != nil || out["workout"] == nil {
		return loggedWorkout{}, errors.New("the result has no workout")
	}
	if problems := m.v.Validate(raw); len(problems) > 0 {
		return loggedWorkout{}, fmt.Errorf("the workout isn't valid OWS: %s", problems[0])
	}
	var w loggedWorkout
	if err := json.Unmarshal(raw, &w); err != nil || w.ID == "" {
		return loggedWorkout{}, errors.New("the workout has no id")
	}
	return w, nil
}

// replay: the same call with the same idempotency_key returns the same workout, not a second one.
func (m *mcpRunner) replay(ctx context.Context) error {
	out, err := m.call(ctx, m.session, "log_workout", m.args)
	if err != nil {
		return err
	}
	w, err := m.workout(out)
	if err != nil {
		return err
	}
	if w.ID != m.id {
		return fmt.Errorf("the same idempotency_key stored a second workout %s (first %s)", w.ID, m.id)
	}
	return nil
}

// needs-confirmation: an exercise no catalogue knows is returned in needs_confirmation with its
// entry ID, so the assistant asks the person instead of guessing.
func (m *mcpRunner) needsConfirmation(context.Context) error {
	list, _ := m.logged["needs_confirmation"].([]any)
	for _, c := range list {
		if c, ok := c.(map[string]any); ok && c["entry_id"] == m.unmatched {
			return nil
		}
	}
	return fmt.Errorf("needs_confirmation %v doesn't list entry %s, an exercise no catalogue knows", list, m.unmatched)
}

// read: get_workouts lists the logged workout, valid.
func (m *mcpRunner) read(ctx context.Context) error {
	found, err := m.listed(ctx)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("get_workouts doesn't list workout %s", m.id)
	}
	return nil
}

func (m *mcpRunner) listed(ctx context.Context) (bool, error) {
	out, err := m.call(ctx, m.session, "get_workouts", map[string]any{})
	if err != nil {
		return false, err
	}
	list, _ := out["workouts"].([]any)
	for _, w := range list {
		raw, _ := json.Marshal(w)
		if problems := m.v.Validate(raw); len(problems) > 0 {
			return false, fmt.Errorf("get_workouts returned an invalid workout: %s", problems[0])
		}
		if w, ok := w.(map[string]any); ok && w["id"] == m.id {
			return true, nil
		}
	}
	return false, nil
}

// errors: failures are tool errors whose text starts with a profile code and a colon: an unknown
// undo token is undo_unavailable, and deleting an ID that doesn't exist names a profile code.
func (m *mcpRunner) errors(ctx context.Context) error {
	text, err := m.fails(ctx, m.session, "undo", map[string]any{"undo_token": "ows-conformance-not-a-token-" + m.run})
	if err != nil {
		return err
	}
	if !strings.HasPrefix(text, "undo_unavailable: ") {
		return fmt.Errorf("undo with an unknown token: %q, want undo_unavailable", clip([]byte(text)))
	}
	text, err = m.fails(ctx, m.session, "delete_entry", map[string]any{"entry_id": "ows-conformance-missing-" + m.run})
	if err != nil {
		return err
	}
	code, _, _ := strings.Cut(text, ": ")
	if !m.codes[code] {
		return fmt.Errorf("delete_entry with an unknown ID: %q doesn't start with a profile error code", clip([]byte(text)))
	}
	return nil
}

// read-only: a read-only credential lists only tools annotated readOnlyHint, the core's reads
// among them, and can't log.
func (m *mcpRunner) readOnly(ctx context.Context) error {
	s, err := m.connect(ctx, m.cfg.ReadOnlyToken)
	if err != nil {
		return fmt.Errorf("connect with the read-only token: %w", err)
	}
	defer s.Close()
	listed, err := listAll(ctx, s)
	if err != nil {
		return err
	}
	for name, t := range listed {
		if t.Annotations == nil || !t.Annotations.ReadOnlyHint {
			return fmt.Errorf("a read-only credential is shown %s, which isn't read-only", name)
		}
	}
	for _, t := range m.core {
		if _, ok := listed[t.Name]; t.Annotations != nil && t.Annotations.ReadOnlyHint && !ok {
			return fmt.Errorf("a read-only credential isn't shown %s, a core read", t.Name)
		}
	}
	res, err := s.CallTool(ctx, &mcp.CallToolParams{Name: "log_workout", Arguments: map[string]any{
		"exercises": []any{map[string]any{"name": "Zzqx Read Only", "sets": []any{map[string]any{"reps": 1}}}},
	}})
	if err == nil && !res.IsError {
		return errors.New("a read-only credential logged a workout")
	}
	return nil
}

// delete: delete_entry removes the workout and returns an undo_token; get_workouts no longer
// lists it.
func (m *mcpRunner) delete(ctx context.Context) error {
	out, err := m.call(ctx, m.session, "delete_entry", map[string]any{"entry_id": m.id})
	if err != nil {
		return err
	}
	m.deleted = true
	if token, _ := out["undo_token"].(string); token == "" {
		return errors.New("delete_entry returned no undo_token")
	}
	found, err := m.listed(ctx)
	if err != nil {
		return err
	}
	if found {
		return fmt.Errorf("get_workouts still lists deleted workout %s", m.id)
	}
	return nil
}

func (m *mcpRunner) cleanUp(ctx context.Context) {
	if m.id == "" || m.deleted || m.session == nil {
		return
	}
	_, _ = m.call(context.WithoutCancel(ctx), m.session, "delete_entry", map[string]any{"entry_id": m.id})
}

// call runs a tool that should succeed and returns its result as JSON, checking the shape the
// profile gives every result: a summary to echo, then the JSON.
func (m *mcpRunner) call(ctx context.Context, s *mcp.ClientSession, tool string, args map[string]any) (map[string]any, error) {
	res, err := s.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", tool, err)
	}
	if res.IsError {
		return nil, fmt.Errorf("%s: %s", tool, clip([]byte(text(res, 0))))
	}
	if len(res.Content) < 2 || text(res, 0) == "" {
		return nil, fmt.Errorf("%s: want a summary, then the result as JSON, in two text blocks", tool)
	}
	var out map[string]any
	dec := json.NewDecoder(bytes.NewReader([]byte(text(res, 1))))
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("%s: the second block isn't a JSON object: %v", tool, err)
	}
	return out, nil
}

// fails runs a tool that should fail and returns the tool error's text.
func (m *mcpRunner) fails(ctx context.Context, s *mcp.ClientSession, tool string, args map[string]any) (string, error) {
	res, err := s.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return "", fmt.Errorf("%s: a protocol error (%v), want a tool error", tool, err)
	}
	if !res.IsError {
		return "", fmt.Errorf("%s succeeded, want a tool error", tool)
	}
	return text(res, 0), nil
}

func text(res *mcp.CallToolResult, i int) string {
	if i >= len(res.Content) {
		return ""
	}
	if t, ok := res.Content[i].(*mcp.TextContent); ok {
		return t.Text
	}
	return ""
}
