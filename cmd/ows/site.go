package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
)

//go:embed templates/*.html templates/style.css
var templateFS embed.FS

type page struct {
	Title       string
	Description string
	Path        string
	HTML        template.HTML
	Schemas     []schemaDoc
	Profile     *profile
	API         *apiDoc
}

// schemaDoc is one JSON Schema file, read generically for rendering.
type schemaDoc struct {
	Kind        string
	ID          string
	Title       string
	Description string
	Root        objectDoc
	Defs        []objectDoc
}

type objectDoc struct {
	Name        string
	Description string
	Properties  []propertyDoc
}

type propertyDoc struct {
	Name        string
	Type        string
	Required    bool
	Description string
}

// profile is mcp/tools.json as the reference server publishes it. Each tool's _meta names its
// profile: "core", or the reference server's own extensions.
type profile struct {
	Profile               string `json:"profile"`
	Version               string `json:"version"`
	Source                string `json:"source"`
	Instructions          string `json:"instructions"`
	ExtensionInstructions string `json:"extension_instructions"`
	ErrorCodes            []struct {
		Code    string `json:"code"`
		Profile string `json:"profile"`
		Meaning string `json:"meaning"`
	} `json:"error_codes"`
	Tools []struct {
		Name        string         `json:"name"`
		Title       string         `json:"title"`
		Description string         `json:"description"`
		InputSchema map[string]any `json:"inputSchema"`
		Annotations struct {
			ReadOnlyHint bool `json:"readOnlyHint"`
		} `json:"annotations"`
		Meta struct {
			Profile string `json:"dev.openworkout/profile"`
		} `json:"_meta"`
	} `json:"tools"`
}

func buildSite(root, out string) error {
	schemas, err := loadSchemas(filepath.Join(root, "schema", "v0"))
	if err != nil {
		return err
	}
	var prof profile
	profRaw, err := os.ReadFile(filepath.Join(root, "mcp", "tools.json"))
	if err != nil {
		return fmt.Errorf("read tool profile: %w", err)
	}
	if err := json.Unmarshal(profRaw, &prof); err != nil {
		return fmt.Errorf("decode tool profile: %w", err)
	}

	tmpl, err := template.New("").Funcs(template.FuncMap{
		"inputs": inputProperties,
		"lines":  func(s string) []string { return strings.Split(s, "\n") },
	}).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return fmt.Errorf("parse templates: %w", err)
	}
	if err := os.RemoveAll(out); err != nil {
		return fmt.Errorf("clear output: %w", err)
	}
	render := func(path, name string, p page) error {
		p.Path = path
		dest := filepath.Join(out, path, "index.html")
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		var b bytes.Buffer
		if err := tmpl.ExecuteTemplate(&b, name, p); err != nil {
			return fmt.Errorf("render %s: %w", path, err)
		}
		return os.WriteFile(dest, b.Bytes(), 0o644)
	}

	if err := render("", "home.html", page{Title: "Open Workout", Description: "An open format for a workout log, and the MCP tools an assistant uses to read and write one.", Schemas: schemas, Profile: &prof}); err != nil {
		return err
	}
	if err := render("schema", "schema.html", page{Title: "Schema", Description: "The Open Workout Schema, version 0 draft: a workout and a program.", Schemas: schemas}); err != nil {
		return err
	}
	api, err := loadAPIDoc(filepath.Join(root, "api", "core.yaml"))
	if err != nil {
		return err
	}
	if err := render("api", "api.html", page{Title: "API core", Description: "The routes every Open Workout server serves: workouts, sets and entries, undo, the change feed, export, webhooks and exercise search.", API: api}); err != nil {
		return err
	}
	// The quickstarts, with links between them made site paths.
	quickstarts := map[string]string{"": "Quickstarts", "typescript": "TypeScript quickstart", "go": "Go quickstart", "mcp": "MCP quickstart", "matching": "Matching quickstart"}
	for name, title := range quickstarts {
		file := name + ".md"
		if name == "" {
			file = "README.md"
		}
		html, err := renderMarkdown(filepath.Join(root, "quickstarts", file))
		if err != nil {
			return err
		}
		html = template.HTML(quickstartLinks.ReplaceAllStringFunc(string(html), func(href string) string {
			target := quickstartLinks.FindStringSubmatch(href)
			if target[1] == "README" {
				return `href="/quickstarts/` + target[2] + `"`
			}
			return `href="/quickstarts/` + target[1] + `/` + target[2] + `"`
		}))
		html = template.HTML(repoLinks.ReplaceAllString(string(html), `href="https://github.com/openexercise/open-workout/tree/main/$1"`))
		if err := render(filepath.Join("quickstarts", name), "doc.html", page{Title: title, Description: title + " for Open Workout.", HTML: html}); err != nil {
			return err
		}
	}
	if err := render("mcp", "mcp.html", page{Title: "MCP tool profile", Description: "The tools an assistant uses over an Open Workout log, as the reference server lists them.", Profile: &prof}); err != nil {
		return err
	}
	for name, path := range map[string]string{"changelog": filepath.Join(root, "schema", "CHANGELOG.md"), "mcp/profile": filepath.Join(root, "mcp", "README.md")} {
		html, err := renderMarkdown(path)
		if err != nil {
			return err
		}
		title := "Changelog"
		if name == "mcp/profile" {
			title = "What a compatible server keeps to"
		}
		if err := render(name, "doc.html", page{Title: title, Description: title + ".", HTML: html}); err != nil {
			return err
		}
	}

	// The schema files at their $id paths, the progression rules' and webhook signatures' worked
	// cases, the profile, the
	// examples, and the stylesheet.
	copies := map[string]string{
		"schema/v0/workout.json":                   filepath.Join(root, "schema", "v0", "workout.json"),
		"schema/v0/program.json":                   filepath.Join(root, "schema", "v0", "program.json"),
		"schema/v0/routine.json":                   filepath.Join(root, "schema", "v0", "routine.json"),
		"schema/v0/log.json":                       filepath.Join(root, "schema", "v0", "log.json"),
		"schema/v0/progression-vectors.json":       filepath.Join(root, "schema", "v0", "progression-vectors.json"),
		"schema/v0/webhook-signature-vectors.json": filepath.Join(root, "schema", "v0", "webhook-signature-vectors.json"),
		"mcp/tools.json":                           filepath.Join(root, "mcp", "tools.json"),
		"api/core.yaml":                            filepath.Join(root, "api", "core.yaml"),
	}
	examples, _ := filepath.Glob(filepath.Join(root, "examples", "*.json"))
	for _, e := range examples {
		copies["examples/"+filepath.Base(e)] = e
	}
	for dest, src := range copies {
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := writeFile(filepath.Join(out, dest), data); err != nil {
			return err
		}
	}
	css, err := templateFS.ReadFile("templates/style.css")
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(out, "style.css"), css); err != nil {
		return err
	}
	fmt.Printf("site: %d schemas, %d tools, %d examples in %s\n", len(schemas), len(prof.Tools), len(examples), out)
	return nil
}

func loadSchemas(dir string) ([]schemaDoc, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	// The workout is the primary document and comes first; the rest by name.
	sort.Slice(paths, func(i, j int) bool {
		wi, wj := filepath.Base(paths[i]) == "workout.json", filepath.Base(paths[j]) == "workout.json"
		if wi != wj {
			return wi
		}
		return paths[i] < paths[j]
	})
	var docs []schemaDoc
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var s map[string]any
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("decode %s: %w", p, err)
		}
		if str(s["$id"]) == "" {
			continue // Not a schema: the progression rules' worked cases sit beside them.
		}
		doc := schemaDoc{
			Kind: strings.TrimSuffix(filepath.Base(p), ".json"), ID: str(s["$id"]), Title: str(s["title"]), Description: str(s["description"]),
			Root: objectFor(strings.TrimSuffix(filepath.Base(p), ".json"), s),
		}
		if defs, ok := s["$defs"].(map[string]any); ok {
			names := make([]string, 0, len(defs))
			for n := range defs {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				if d, ok := defs[n].(map[string]any); ok {
					doc.Defs = append(doc.Defs, objectFor(n, d))
				}
			}
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

func objectFor(name string, s map[string]any) objectDoc {
	o := objectDoc{Name: name, Description: str(s["description"])}
	required := map[string]bool{}
	if req, ok := s["required"].([]any); ok {
		for _, r := range req {
			required[str(r)] = true
		}
	}
	props, _ := s["properties"].(map[string]any)
	names := make([]string, 0, len(props))
	for n := range props {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		p, _ := props[n].(map[string]any)
		o.Properties = append(o.Properties, propertyDoc{Name: n, Type: typeOf(p), Required: required[n], Description: str(p["description"])})
	}
	return o
}

// typeOf describes a property's shape in a few words.
func typeOf(p map[string]any) string {
	if p == nil {
		return ""
	}
	switch {
	case p["$ref"] != nil:
		return strings.TrimPrefix(str(p["$ref"]), "#/$defs/")
	case p["const"] != nil:
		return fmt.Sprintf("the constant %v", p["const"])
	case p["enum"] != nil:
		parts := []string{}
		if vals, ok := p["enum"].([]any); ok {
			for _, v := range vals {
				parts = append(parts, fmt.Sprint(v))
			}
		}
		return "one of " + strings.Join(parts, ", ")
	}
	var types []string
	switch t := p["type"].(type) {
	case string:
		types = []string{t}
	case []any:
		for _, v := range t {
			types = append(types, str(v))
		}
	}
	out := strings.Join(types, " or ")
	if items, ok := p["items"].(map[string]any); ok && out == "array" {
		out = "array of " + typeOf(items)
	}
	if f := str(p["format"]); f != "" {
		out += " (" + f + ")"
	}
	return out
}

// inputProperties flattens a tool's input schema for the profile page.
func inputProperties(schema map[string]any) []propertyDoc {
	return objectFor("", schema).Properties
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

var (
	// quickstartLinks are links from one quickstart to another (go.md, README.md#where-to-run-them).
	quickstartLinks = regexp.MustCompile(`href="(README|typescript|go|mcp|matching)\.md(#[^"]*)?"`)
	// repoLinks leave the quickstarts for the repository (../partner-app, ../mcp/README.md).
	repoLinks = regexp.MustCompile(`href="\.\./([^"]*)"`)
)

func renderMarkdown(path string) (template.HTML, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := goldmark.New().Convert(src, &b); err != nil {
		return "", fmt.Errorf("render %s: %w", path, err)
	}
	return template.HTML(b.String()), nil // our own file in this repository
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
