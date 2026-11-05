// Command template is a small Open Workout MCP server to copy: it serves the profile's core
// tools (../tools.json) over Streamable HTTP, keeping each person's log in memory. It is a
// starting point, not a hosted service: replace the store with a database and the token check
// with OAuth (README.md says what that takes).
package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	addr := flag.String("addr", "localhost:8080", "address to listen on")
	unit := flag.String("unit", "kg", "the unit a load without one is in: kg or lb")
	tz := flag.String("tz", "Local", "the person's timezone, for days")
	flag.Parse()

	if *unit != "kg" && *unit != "lb" {
		log.Fatal("-unit must be kg or lb")
	}
	loc, err := time.LoadLocation(*tz)
	if err != nil {
		log.Fatalf("-tz: %v", err)
	}
	catalogue, err := LoadCatalogue()
	if err != nil {
		log.Fatal(err)
	}
	srv, err := NewServer(NewStore(catalogue, *unit, loc))
	if err != nil {
		log.Fatal(err)
	}

	token := os.Getenv("OWS_TEMPLATE_TOKEN")
	if token == "" {
		token = rand.Text()
		log.Printf("OWS_TEMPLATE_TOKEN isn't set; this run's token is %s", token)
	}
	log.Printf("serving the Open Workout MCP core on http://%s/", *addr)
	log.Fatal(http.ListenAndServe(*addr, newHandler(srv, token)))
}

// newHandler serves MCP over Streamable HTTP, stateless with JSON responses so any instance can
// answer any request, behind the bearer token.
func newHandler(srv *mcp.Server, token string) http.Handler {
	streamable := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	return auth.RequireBearerToken(verifier(token), nil)(streamable)
}

// verifier accepts one token, for one person. A real server issues OAuth access tokens
// (OAuth 2.1 with PKCE, discovered from a 401 through RFC 9728 metadata) and names the person
// each one belongs to.
func verifier(token string) auth.TokenVerifier {
	return func(_ context.Context, got string, _ *http.Request) (*auth.TokenInfo, error) {
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			return nil, auth.ErrInvalidToken
		}
		return &auth.TokenInfo{UserID: "me", Scopes: []string{"mcp"}, Expiration: time.Now().Add(time.Hour)}, nil
	}
}
