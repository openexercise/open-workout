package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	linkAttrs = regexp.MustCompile(`(?:href|src)="([^"]*)"`)
	idAttrs   = regexp.MustCompile(`id="([^"]*)"`)
)

// TestSiteLinks builds the site and checks that every link on every page leads somewhere: no
// relative link to a Markdown file (GitHub's view of the repository, not the site's), every site
// path served, and every #anchor on a site page present on that page.
func TestSiteLinks(t *testing.T) {
	out := t.TempDir()
	if err := buildSite(filepath.Join("..", ".."), out); err != nil {
		t.Fatal(err)
	}
	ids := map[string]map[string]bool{} // page path ("/guide/") -> its element IDs
	pages := map[string]string{}
	err := filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(p) != ".html" {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(out, filepath.Dir(p))
		page := "/"
		if rel != "." {
			page = "/" + filepath.ToSlash(rel) + "/"
		}
		pages[page] = string(data)
		ids[page] = map[string]bool{}
		for _, m := range idAttrs.FindAllStringSubmatch(string(data), -1) {
			ids[page][m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := pages["/guide/"]; !ok {
		t.Fatal("the developer guide isn't on the site")
	}
	for page, html := range pages {
		for _, m := range linkAttrs.FindAllStringSubmatch(html, -1) {
			href := m[1]
			if strings.Contains(href, "://") || strings.HasPrefix(href, "mailto:") {
				continue
			}
			target, fragment, _ := strings.Cut(href, "#")
			switch {
			case target == "":
				target = page
			case !strings.HasPrefix(target, "/"):
				t.Errorf("%s: relative link %q", page, href)
				continue
			}
			if strings.HasSuffix(target, "/") {
				if _, ok := pages[target]; !ok {
					t.Errorf("%s: link %q: no such page", page, href)
					continue
				}
				if fragment != "" && !ids[target][fragment] {
					t.Errorf("%s: link %q: no #%s on %s", page, href, fragment, target)
				}
				continue
			}
			if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(target))); err != nil {
				t.Errorf("%s: link %q: no such file", page, href)
			}
		}
	}
}

func TestSiteLink(t *testing.T) {
	root := filepath.Join("..", "..")
	cases := []struct{ from, href, want string }{
		{"docs/developer-guide.md", "../schema/v0/SPEC.md", "/schema/v0/spec/"},
		{"docs/developer-guide.md", "../quickstarts/README.md", "/quickstarts/"},
		{"docs/developer-guide.md", "../quickstarts/go.md", "/quickstarts/go/"},
		{"docs/developer-guide.md", "../examples/push-day.json", "/examples/push-day.json"},
		{"docs/developer-guide.md", "../mcp/template", repoURL + "/tree/main/mcp/template"},
		{"docs/developer-guide.md", "../README.md#check-a-server", repoURL + "/blob/main/README.md#check-a-server"},
		{"docs/developer-guide.md", "#5-estimates", "#5-estimates"},
		{"quickstarts/go.md", "README.md#where-to-run-them", "/quickstarts/#where-to-run-them"},
		{"quickstarts/README.md", "../docs/developer-guide.md", "/guide/"},
		{"schema/v0/SPEC.md", "../CHANGELOG.md", "/changelog/"},
	}
	for _, c := range cases {
		got, err := siteLink(root, c.from, c.href)
		if err != nil || got != c.want {
			t.Errorf("siteLink(%q, %q) = %q, %v; want %q", c.from, c.href, got, err, c.want)
		}
	}
	if _, err := siteLink(root, "docs/developer-guide.md", "../no-such-file.md"); err == nil {
		t.Error("a link to a missing file passed")
	}
}
