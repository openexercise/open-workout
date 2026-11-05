package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestOverHTTP runs the server as main does: a request without the token is refused, and a
// client with it logs a workout over Streamable HTTP as the token's person.
func TestOverHTTP(t *testing.T) {
	catalogue, err := LoadCatalogue()
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(catalogue, "kg", time.UTC)
	srv, err := NewServer(store)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(newHandler(srv, tokenSet{write: "secret", read: "reader"}, "http://template.test"))
	defer ts.Close()

	res, err := http.Post(ts.URL, "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without a token: %d, want 401", res.StatusCode)
	}

	ctx := context.Background()
	transport := &mcp.StreamableClientTransport{Endpoint: ts.URL, HTTPClient: &http.Client{Transport: bearer{"secret"}}}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "http-client", Version: "1"}, nil).Connect(ctx, transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	out, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "log_workout", Arguments: map[string]any{
		"exercises": []any{map[string]any{"name": "deadlift", "sets": []any{map[string]any{"reps": 3, "load": 180}}}},
	}})
	if err != nil || out.IsError {
		t.Fatalf("log_workout over HTTP: %v %v", err, out)
	}
	if _, ok := store.people["me"]; !ok {
		t.Fatal("the workout wasn't stored as the token's person")
	}
}

type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}
