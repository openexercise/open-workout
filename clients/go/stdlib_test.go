package lifterclub

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStandardLibraryOnly keeps the package free of imports outside the standard library, so
// the copy published in openexercise/open-workout builds unchanged there, and it never reaches
// into Lifterclub's internals.
func TestStandardLibraryOnly(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".go" {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), e.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if before, _, _ := strings.Cut(path, "/"); strings.Contains(before, ".") {
				t.Errorf("%s imports %s, which isn't the standard library", e.Name(), path)
			}
		}
	}
}
