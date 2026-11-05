package main

import (
	_ "embed"
	"encoding/json"
	"slices"
	"strings"
	"unicode"
)

// catalogue.json is a handful of Open Exercise records, enough to show matching. A real server
// loads the whole catalogue from https://openexercise.org and keeps each person's own exercises.
//
//go:embed catalogue.json
var catalogueJSON []byte

// CatalogueExercise is an exercise the server can match a name to.
type CatalogueExercise struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Aliases        []string `json:"aliases"`
	Equipment      []string `json:"equipment"`
	PrimaryMuscles []string `json:"primary_muscles"`
}

// Catalogue matches what people call exercises to catalogue IDs.
type Catalogue struct {
	exercises []CatalogueExercise
}

// LoadCatalogue reads the embedded sample catalogue.
func LoadCatalogue() (*Catalogue, error) {
	var c Catalogue
	if err := json.Unmarshal(catalogueJSON, &c.exercises); err != nil {
		return nil, err
	}
	return &c, nil
}

// ByID returns the exercise with an ID, or nil.
func (c *Catalogue) ByID(id string) *CatalogueExercise {
	for i := range c.exercises {
		if c.exercises[i].ID == id {
			return &c.exercises[i]
		}
	}
	return nil
}

type scored struct {
	exercise *CatalogueExercise
	score    float64
}

// Match returns the exercise a name means exactly (its name or an alias, ignoring case,
// punctuation and plurals), or else the close candidates, best first.
func (c *Catalogue) Match(name string) (exact *CatalogueExercise, candidates []*CatalogueExercise) {
	want := normalize(name)
	for i := range c.exercises {
		for _, n := range c.names(i) {
			if normalize(n) == want {
				return &c.exercises[i], nil
			}
		}
	}
	for _, s := range c.rank(name, 0.5) {
		candidates = append(candidates, s.exercise)
		if len(candidates) == 3 {
			break
		}
	}
	return nil, candidates
}

// rank scores every exercise against a name by shared words, keeping those at or above min.
func (c *Catalogue) rank(name string, min float64) []scored {
	words := strings.Fields(normalize(name))
	var out []scored
	for i := range c.exercises {
		best := 0.0
		for _, n := range c.names(i) {
			best = max(best, overlap(words, strings.Fields(normalize(n))))
		}
		if best >= min {
			out = append(out, scored{&c.exercises[i], best})
		}
	}
	slices.SortStableFunc(out, func(a, b scored) int {
		switch {
		case a.score > b.score:
			return -1
		case a.score < b.score:
			return 1
		}
		return 0
	})
	return out
}

func (c *Catalogue) names(i int) []string {
	return append([]string{c.exercises[i].Name}, c.exercises[i].Aliases...)
}

// overlap is the share of words two names have in common, over the longer name.
func overlap(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	shared := 0
	for _, w := range a {
		if slices.Contains(b, w) {
			shared++
		}
	}
	return float64(shared) / float64(max(len(a), len(b)))
}

// normalize lowercases a name, keeps letters and digits of any script, and drops a plural s.
func normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	words := strings.Fields(b.String())
	for i, w := range words {
		if len(w) > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") {
			words[i] = strings.TrimSuffix(w, "s")
		}
	}
	return strings.Join(words, " ")
}
