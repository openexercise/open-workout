// Command ows validates Open Workout Schema documents, checks a server's REST API for
// conformance, and builds the website.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/openexercise/open-workout/conformance"
	"github.com/openexercise/open-workout/internal/ows"
)

const usage = `usage:
  ows validate [path...]                         validate documents (examples/ by default)
  ows conformance [--json] [--token T] <base-url>  run the conformance script against a server
  ows site [outdir]                              build the website (dist/ by default)`

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
		err = runConformance(os.Args[2:])
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
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
	} else {
		counts := map[conformance.Status]int{}
		for _, r := range report.Results {
			counts[r.Status]++
			line := strings.ToUpper(string(r.Status)) + " " + r.Name
			if r.Detail != "" {
				line += ": " + r.Detail
			}
			fmt.Println(line)
		}
		fmt.Printf("%d checks: %d passed, %d failed, %d skipped\n", len(report.Results), counts[conformance.Pass], counts[conformance.Fail], counts[conformance.Skip])
	}
	if !report.Passed() {
		return errors.New("not conformant")
	}
	return nil
}
