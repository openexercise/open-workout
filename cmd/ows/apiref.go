package main

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"gopkg.in/yaml.v3"
)

// The API reference page, rendered from api/core.yaml: every operation by tag, with its
// parameters, body, responses and scope, then every schema. Read generically, so a new field or
// route in the document shows without a change here.

type apiDoc struct {
	Title       string
	Version     string
	Description template.HTML
	Tags        []apiTag
	Schemas     []apiSchema
}

type apiTag struct {
	Name       string
	Operations []apiOperation
}

type apiOperation struct {
	ID          string
	Method      string
	Path        string
	Summary     string
	Description template.HTML
	Scopes      string
	Parameters  []apiParam
	Body        template.HTML
	Responses   []apiResponse
}

type apiParam struct {
	Name, In, Type string
	Description    template.HTML
	Required       bool
}

type apiResponse struct {
	Status      string
	Description template.HTML
	Schema      template.HTML
}

type apiSchema struct {
	Name        string
	Description template.HTML
	Type        template.HTML
	Properties  []apiProperty
}

type apiProperty struct {
	Name        string
	Type        template.HTML
	Required    bool
	Description template.HTML
}

var methods = []string{"get", "post", "put", "patch", "delete"}

func loadAPIDoc(path string) (*apiDoc, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read the API document: %w", err)
	}
	var spec map[string]any
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		return nil, fmt.Errorf("decode the API document: %w", err)
	}
	info := m(spec["info"])
	doc := &apiDoc{Title: str(info["title"]), Version: str(info["version"]), Description: md(str(info["description"]))}
	components := m(spec["components"])

	byTag := map[string][]apiOperation{}
	var tagOrder []string
	for _, t := range list(spec["tags"]) {
		tagOrder = append(tagOrder, str(m(t)["name"]))
	}
	paths := m(spec["paths"])
	pathNames := make([]string, 0, len(paths))
	for p := range paths {
		pathNames = append(pathNames, p)
	}
	sort.Strings(pathNames)
	for _, p := range pathNames {
		item := m(paths[p])
		for _, method := range methods {
			op := m(item[method])
			if op == nil {
				continue
			}
			o := apiOperation{ID: str(op["operationId"]), Method: strings.ToUpper(method), Path: p, Summary: str(op["summary"]), Description: md(str(op["description"]))}
			for _, sec := range list(op["security"]) {
				for _, scopes := range m(sec) {
					var names []string
					for _, s := range list(scopes) {
						names = append(names, str(s))
					}
					o.Scopes = strings.Join(names, " ")
				}
			}
			for _, prm := range append(list(item["parameters"]), list(op["parameters"])...) {
				prm = resolve(components, prm)
				pm := m(prm)
				o.Parameters = append(o.Parameters, apiParam{Name: str(pm["name"]), In: str(pm["in"]), Required: pm["required"] == true,
					Type: plainType(m(pm["schema"])), Description: inline(str(pm["description"]))})
			}
			if body := m(resolve(components, op["requestBody"])); body != nil {
				o.Body = schemaRef(m(m(m(body["content"])["application/json"])["schema"]))
			}
			responses := m(op["responses"])
			codes := make([]string, 0, len(responses))
			for c := range responses {
				codes = append(codes, c)
			}
			sort.Strings(codes)
			for _, c := range codes {
				r := m(resolve(components, responses[c]))
				var schema template.HTML
				for ctype, content := range m(r["content"]) {
					if ref := schemaRef(m(m(content)["schema"])); ref != "" {
						schema = template.HTML(template.HTMLEscapeString(ctype)+": ") + ref
					}
				}
				o.Responses = append(o.Responses, apiResponse{Status: c, Description: inline(str(r["description"])), Schema: schema})
			}
			tags := list(op["tags"])
			tag := "Other"
			if len(tags) > 0 {
				tag = str(tags[0])
			}
			if _, seen := byTag[tag]; !seen && !contains(tagOrder, tag) {
				tagOrder = append(tagOrder, tag)
			}
			byTag[tag] = append(byTag[tag], o)
		}
	}
	for _, t := range tagOrder {
		if ops := byTag[t]; len(ops) > 0 {
			doc.Tags = append(doc.Tags, apiTag{Name: t, Operations: ops})
		}
	}

	schemas := m(components["schemas"])
	names := make([]string, 0, len(schemas))
	for n := range schemas {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		s := m(schemas[n])
		out := apiSchema{Name: n, Description: md(str(s["description"]))}
		props := m(s["properties"])
		if len(props) == 0 {
			out.Type = schemaRef(s)
		}
		required := map[string]bool{}
		for _, r := range list(s["required"]) {
			required[str(r)] = true
		}
		propNames := make([]string, 0, len(props))
		for p := range props {
			propNames = append(propNames, p)
		}
		sort.Slice(propNames, func(i, j int) bool {
			if required[propNames[i]] != required[propNames[j]] {
				return required[propNames[i]]
			}
			return propNames[i] < propNames[j]
		})
		for _, p := range propNames {
			ps := m(props[p])
			out.Properties = append(out.Properties, apiProperty{Name: p, Type: schemaRef(ps), Required: required[p], Description: inline(str(ps["description"]))})
		}
		doc.Schemas = append(doc.Schemas, out)
	}
	return doc, nil
}

// resolve follows a local $ref into components (#/components/<kind>/<name>).
func resolve(components map[string]any, v any) any {
	ref := str(m(v)["$ref"])
	if !strings.HasPrefix(ref, "#/components/") {
		return v
	}
	parts := strings.Split(strings.TrimPrefix(ref, "#/components/"), "/")
	if len(parts) != 2 {
		return v
	}
	return m(components[parts[0]])[parts[1]]
}

// schemaRef names a schema: a link for a component, the type otherwise, arrays and unions included.
func schemaRef(s map[string]any) template.HTML {
	if s == nil {
		return ""
	}
	if ref := str(s["$ref"]); ref != "" {
		name := ref[strings.LastIndex(ref, "/")+1:]
		if strings.HasPrefix(ref, "#/components/schemas/") {
			return template.HTML(fmt.Sprintf(`<a href="#schema-%s"><code>%s</code></a>`, template.HTMLEscapeString(name), template.HTMLEscapeString(name)))
		}
		return template.HTML("<code>" + template.HTMLEscapeString(strings.TrimSuffix(name, ".json")) + "</code> (" + template.HTMLEscapeString(ref) + ")")
	}
	for _, key := range []string{"oneOf", "anyOf"} {
		if alts := list(s[key]); len(alts) > 0 {
			var parts []string
			for _, a := range alts {
				parts = append(parts, string(schemaRef(m(a))))
			}
			return template.HTML(strings.Join(parts, " or "))
		}
	}
	if enum := list(s["enum"]); len(enum) > 0 {
		var vals []string
		for _, e := range enum {
			vals = append(vals, "<code>"+template.HTMLEscapeString(fmt.Sprint(e))+"</code>")
		}
		return template.HTML("one of " + strings.Join(vals, ", "))
	}
	if str(s["type"]) == "array" {
		return "array of " + schemaRef(m(s["items"]))
	}
	return template.HTML(template.HTMLEscapeString(plainType(s)))
}

func plainType(s map[string]any) string {
	if s == nil {
		return ""
	}
	t := fmt.Sprint(s["type"])
	if s["type"] == nil {
		t = "any"
	}
	if f := str(s["format"]); f != "" {
		t += " (" + f + ")"
	}
	return t
}

func md(s string) template.HTML {
	if s == "" {
		return ""
	}
	var b bytes.Buffer
	if err := goldmark.Convert([]byte(s), &b); err != nil {
		return template.HTML(template.HTMLEscapeString(s))
	}
	return template.HTML(b.String())
}

// inline renders a short description, one paragraph without its <p>, for a table cell.
func inline(s string) template.HTML {
	html := strings.TrimSpace(string(md(s)))
	if strings.Count(html, "<p>") == 1 {
		html = strings.TrimSuffix(strings.TrimPrefix(html, "<p>"), "</p>")
	}
	return template.HTML(html)
}

func m(v any) map[string]any {
	out, _ := v.(map[string]any)
	return out
}

func list(v any) []any {
	out, _ := v.([]any)
	return out
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}
