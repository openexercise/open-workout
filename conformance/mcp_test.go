package conformance

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestTemplatePasses builds the template server (mcp/template, a module of its own), runs it,
// and checks its MCP tools and its API core: the template is what other servers copy, so it must
// pass both.
func TestTemplatePasses(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the template server")
	}
	bin := filepath.Join(t.TempDir(), "template")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = filepath.Join("..", "mcp", "template")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the template: %v\n%s", err, out)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	server := exec.Command(bin, "-addr", addr)
	server.Env = append(os.Environ(), "OWS_TEMPLATE_TOKEN=writer", "OWS_TEMPLATE_READ_TOKEN=reader")
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Process.Kill(); _ = server.Wait() })
	for deadline := time.Now().Add(10 * time.Second); ; {
		if c, err := net.Dial("tcp", addr); err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the template didn't start listening")
		}
		time.Sleep(50 * time.Millisecond)
	}

	report, err := RunMCP(context.Background(), MCPConfig{URL: "http://" + addr + "/", Token: "writer", ReadOnlyToken: "reader"})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range report.Results {
		if r.Status != Pass {
			t.Errorf("%s %s: %s", r.Status, r.Name, r.Detail)
		}
	}
	if !report.Passed() {
		t.Fatal("the template should pass the MCP checks")
	}

	// Its API core passes the REST script too.
	rest, err := Run(context.Background(), Config{BaseURL: "http://" + addr, Token: "writer"})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rest.Results {
		if r.Status != Pass {
			t.Errorf("REST %s %s: %s", r.Status, r.Name, r.Detail)
		}
	}
	if !rest.Passed() {
		t.Fatal("the template should pass the REST checks")
	}
}

// A server that offers none of the core fails tools and log, and the checks needing a workout
// skip rather than repeat the failure.
func TestMCPChecksCatchAMissingCore(t *testing.T) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "empty", Version: "0"}, nil)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	verify := func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		if token != "good" {
			return nil, auth.ErrInvalidToken
		}
		return &auth.TokenInfo{UserID: "me", Expiration: time.Now().Add(time.Hour)}, nil
	}
	ts := httptest.NewServer(auth.RequireBearerToken(verify, &auth.RequireBearerTokenOptions{ResourceMetadataURL: "http://x/.well-known/oauth-protected-resource"})(handler))
	defer ts.Close()

	report, err := RunMCP(context.Background(), MCPConfig{URL: ts.URL, Token: "good", HTTPClient: ts.Client()})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Status{"discovery": Pass, "tools": Fail, "log": Fail, "replay": Skip, "needs-confirmation": Skip, "read": Skip, "errors": Fail, "read-only": Skip, "delete": Skip}
	for _, r := range report.Results {
		if r.Status != want[r.Name] {
			t.Errorf("%s = %s, want %s (%s)", r.Name, r.Status, want[r.Name], r.Detail)
		}
	}
	if report.Passed() {
		t.Error("an empty server passed")
	}
	if _, err := RunMCP(context.Background(), MCPConfig{URL: "ftp://x", Token: "t"}); err == nil {
		t.Error("a bad URL was accepted")
	}
}
