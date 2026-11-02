// Command ows validates Open Workout Schema documents and builds the website.
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/openexercise/open-workout/internal/ows"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: ows validate [path...] | ows site [outdir]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "validate":
		err = validate(os.Args[2:])
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
