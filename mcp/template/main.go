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
	"github.com/modelcontextprotocol/go-sdk/oauthex"
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

	tokens := tokenSet{write: os.Getenv("OWS_TEMPLATE_TOKEN"), read: os.Getenv("OWS_TEMPLATE_READ_TOKEN")}
	if tokens.write == "" {
		tokens.write = rand.Text()
		log.Printf("OWS_TEMPLATE_TOKEN isn't set; this run's token is %s", tokens.write)
	}
	if tokens.read == "" {
		tokens.read = rand.Text()
		log.Printf("OWS_TEMPLATE_READ_TOKEN isn't set; this run's read-only token is %s", tokens.read)
	}
	log.Printf("serving the Open Workout MCP core on http://%s/", *addr)
	log.Fatal(http.ListenAndServe(*addr, newHandler(srv, tokens, "http://"+*addr)))
}

// tokenSet is the template's two credentials for its one person: one an assistant logs with, and
// one that may only read.
type tokenSet struct{ write, read string }

// newHandler serves MCP over Streamable HTTP at /, stateless with JSON responses so any instance
// can answer any request, behind the bearer tokens. A request without one gets 401 naming the
// protected resource metadata (RFC 9728), which a client reads to find where to sign in.
func newHandler(srv *mcp.Server, tokens tokenSet, origin string) http.Handler {
	streamable := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	metadataPath := "/.well-known/oauth-protected-resource"
	mux := http.NewServeMux()
	// A real server lists its authorization server here; the template has none, since its tokens
	// are fixed.
	mux.Handle("GET "+metadataPath, auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
		Resource: origin, AuthorizationServers: []string{}, ScopesSupported: []string{"mcp", "mcp:read"}, BearerMethodsSupported: []string{"header"},
	}))
	mux.Handle("/", auth.RequireBearerToken(verifier(tokens), &auth.RequireBearerTokenOptions{ResourceMetadataURL: origin + metadataPath})(streamable))
	return mux
}

// verifier accepts the two tokens, for one person: the read token carries only mcp:read. A real
// server issues OAuth access tokens (OAuth 2.1 with PKCE, discovered from a 401 through RFC 9728
// metadata) and names the person and scopes each one belongs to.
func verifier(tokens tokenSet) auth.TokenVerifier {
	return func(_ context.Context, got string, _ *http.Request) (*auth.TokenInfo, error) {
		scope := ""
		switch {
		case subtle.ConstantTimeCompare([]byte(got), []byte(tokens.write)) == 1:
			scope = "mcp"
		case subtle.ConstantTimeCompare([]byte(got), []byte(tokens.read)) == 1:
			scope = readScope
		default:
			return nil, auth.ErrInvalidToken
		}
		return &auth.TokenInfo{UserID: "me", Scopes: []string{scope}, Expiration: time.Now().Add(time.Hour)}, nil
	}
}
