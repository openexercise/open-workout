// Command ows validates Open Workout Schema documents, checks a server's REST API for
// conformance, and builds the website.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/openexercise/open-workout/conformance"
	"github.com/openexercise/open-workout/internal/ows"
)

const usage = `usage:
  ows validate [path...]                                 validate documents (examples/ by default)
  ows conformance [--json] [--token T] <base-url>        check a server's REST API
  ows conformance mcp [--json] [--token T] [--read-token R] <mcp-url>
                                                         check a server's MCP tools
  ows vectors [--json] -- <command> [args...]            check a progression rules implementation
  ows site [outdir]                                      build the website (dist/ by default)`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "validate":
		err = validate(os.Args[2:])
	case "conformance":
		if len(os.Args) > 2 && os.Args[2] == "mcp" {
			err = runMCPConformance(os.Args[3:])
		} else {
			err = runConformance(os.Args[2:])
		}
	case "vectors":
		err = runVectors(os.Args[2:])
	case "site":
		out := "dist"
		if len(os.Args) > 2 {
			out = os.Args[2]
		}
		err = buildSite(".", out)
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// validate checks every JSON file under the paths (files or directories; examples by default).
func validate(paths []string) error {
	if len(paths) == 0 {
		paths = []string{"examples"}
	}
	v, err := ows.NewValidator(".")
	if err != nil {
		return err
	}
	var problems, count int
	for _, root := range paths {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			count++
			for _, p := range v.Validate(data) {
				problems++
				fmt.Printf("%s: %s\n", path, p)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	if problems > 0 {
		return fmt.Errorf("%d problem(s) in %d document(s)", problems, count)
	}
	fmt.Printf("%d document(s) valid\n", count)
	return nil
}

// runConformance runs the conformance script and prints each check. The token comes from
// --token or OWS_TOKEN; the environment keeps it out of the process list and shell history.
func runConformance(args []string) error {
	flags := flag.NewFlagSet("conformance", flag.ContinueOnError)
	token := flags.String("token", "", "access token with workouts:read and workouts:write (default $OWS_TOKEN)")
	asJSON := flags.Bool("json", false, "print the report as JSON")
	timeout := flags.Duration("timeout", 2*time.Minute, "give up after this long")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "usage: ows conformance [--json] [--token T] <base-url>\n\n"+
			"Logs one workout in the token's account, checks the API's behaviour with it, and deletes it.\n"+
			"Use a test account.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return errors.New("want one base URL, such as https://api.lifterclub.com")
	}
	if *token == "" {
		*token = os.Getenv("OWS_TOKEN")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	report, err := conformance.Run(ctx, conformance.Config{BaseURL: flags.Arg(0), Token: *token})
	if err != nil {
		return err
	}
	if err := printReport(report, report.Results, *asJSON, "checks"); err != nil {
		return err
	}
	if !report.Passed() {
		return errors.New("not conformant")
	}
	return nil
}

// printReport writes a report as JSON, or one line per result and a count.
func printReport(report any, results []conformance.Result, asJSON bool, noun string) error {
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	counts := map[conformance.Status]int{}
	for _, r := range results {
		counts[r.Status]++
		line := strings.ToUpper(string(r.Status)) + " " + r.Name
		if r.Detail != "" {
			line += ": " + r.Detail
		}
		fmt.Println(line)
	}
	fmt.Printf("%d %s: %d passed, %d failed, %d skipped\n", len(results), noun, counts[conformance.Pass], counts[conformance.Fail], counts[conformance.Skip])
	return nil
}

// runMCPConformance checks an MCP server against the tool profile's core. The tokens come from
// flags or OWS_TOKEN and OWS_READ_TOKEN.
func runMCPConformance(args []string) error {
	flags := flag.NewFlagSet("conformance mcp", flag.ContinueOnError)
	token := flags.String("token", "", "token a person's assistant would use, able to write (default $OWS_TOKEN)")
	readToken := flags.String("read-token", "", "a read-only token for the same person (default $OWS_READ_TOKEN); without one, read-only access isn't checked and the server can't pass")
	asJSON := flags.Bool("json", false, "print the report as JSON")
	timeout := flags.Duration("timeout", 2*time.Minute, "give up after this long")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "usage: ows conformance mcp [--json] [--token T] [--read-token R] <mcp-url>\n\n"+
			"Logs one workout in the token's account with the profile's core tools, checks the server's behaviour with it, and deletes it.\n"+
			"Use a test account.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return errors.New("want one MCP endpoint URL, such as https://mcp.lifterclub.com")
	}
	if *token == "" {
		*token = os.Getenv("OWS_TOKEN")
	}
	if *readToken == "" {
		*readToken = os.Getenv("OWS_READ_TOKEN")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	report, err := conformance.RunMCP(ctx, conformance.MCPConfig{URL: flags.Arg(0), Token: *token, ReadOnlyToken: *readToken})
	if err != nil {
		return err
	}
	if err := printReport(report, report.Results, *asJSON, "checks"); err != nil {
		return err
	}
	if !report.Passed() {
		return errors.New("not conformant")
	}
	return nil
}

// runVectors runs every published progression case through a command: the runner contract. The
// command gets one case on stdin, as JSON without want, and prints the target it works out as
// JSON (conformance.VectorTarget) on stdout; it runs once per case.
func runVectors(args []string) error {
	flags := flag.NewFlagSet("vectors", flag.ContinueOnError)
	asJSON := flags.Bool("json", false, "print the report as JSON")
	timeout := flags.Duration("timeout", 10*time.Second, "give up on a case after this long")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "usage: ows vectors [--json] -- <command> [args...]\n\n"+
			"Runs the command once per case in schema/v0/progression-vectors.json, with the case (without want) on stdin,\n"+
			"and compares the JSON target it prints: {\"load\": number|null, \"loads\": [...], \"sets\": [...], \"exercise\": \"...\", \"needs_start\": bool}.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() == 0 {
		flags.Usage()
		return errors.New("want a command to run for each case")
	}
	command := flags.Args()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	report, err := conformance.CheckVectors(ctx, func(ctx context.Context, input json.RawMessage) (conformance.VectorTarget, error) {
		ctx, cancel := context.WithTimeout(ctx, *timeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, command[0], command[1:]...)
		cmd.Stdin = bytes.NewReader(input)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			return conformance.VectorTarget{}, fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
		}
		var target conformance.VectorTarget
		if err := json.Unmarshal(stdout.Bytes(), &target); err != nil {
			return conformance.VectorTarget{}, fmt.Errorf("the output isn't a target: %v", err)
		}
		return target, nil
	})
	if err != nil {
		return err
	}
	if err := printReport(report, report.Results, *asJSON, "cases"); err != nil {
		return err
	}
	if !report.Passed() {
		return errors.New("the implementation disagrees with the published cases")
	}
	return nil
}
