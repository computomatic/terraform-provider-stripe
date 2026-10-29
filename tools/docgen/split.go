package main

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// pageBudget is half the Registry's limit, so that growth is caught long before it truncates a
// page. A page over it moves its largest nested blocks to guide pages of their own (ADR
// 2026-10-29-full-attribute-coverage, D3, "Docs size").
const pageBudget = 250_000

// kindSlug names a page's kind in a guide's name and title.
var kindSlug = map[string]string{"resources": "resource", "data-sources": "data-source"}

// fit renders a resource or data source page within budget: while it is over, the largest
// top-level nested block still on it moves to a guide page of its own, linked from the block's
// line. It returns the page and the guides, keyed by their paths under docs/.
func fit(s *schema, dir string, in pageInput, render func(*schema, pageInput) (string, error), budget int) (map[string]string, error) {
	out := map[string]string{}
	page, err := render(s, in)
	if err != nil {
		return nil, err
	}
	if len(page) > budget {
		in.Guides = map[string]string{}
		blocks := map[string]string{}
		for _, a := range s.Attrs {
			if a.Nesting != nestNone && a.Name != "timeouts" {
				blocks[a.Name] = renderGuide(s, dir, a, in)
			}
		}
		names := slices.Collect(maps.Keys(blocks))
		sort.Slice(names, func(i, j int) bool {
			if len(blocks[names[i]]) != len(blocks[names[j]]) {
				return len(blocks[names[i]]) > len(blocks[names[j]])
			}
			return names[i] < names[j]
		})
		for _, name := range names {
			if len(page) <= budget {
				break
			}
			g := guideName(s, dir, name)
			in.Guides[name] = g
			out[filepath.Join("guides", g+".md")] = blocks[name]
			if page, err = render(s, in); err != nil {
				return nil, err
			}
		}
		if len(page) > budget {
			return nil, fmt.Errorf("%s is %d bytes with every nested block on a guide page, over the budget of %d", pagePath(dir, s.Type), len(page), budget)
		}
	}
	out[pagePath(dir, s.Type)] = page
	return out, nil
}

func guideName(s *schema, dir, attrName string) string {
	return strings.TrimPrefix(s.Type, "stripe_") + "-" + kindSlug[dir] + "-" + attrName
}

// renderGuide is the guide page of one top-level nested block of a page: its subsections, each
// line as the page would write it, the default statements included.
func renderGuide(s *schema, dir string, a *attr, in pageInput) string {
	kind := kindSlug[dir]
	words := strings.ReplaceAll(kind, "-", " ")
	link := fmt.Sprintf("[`%s`](../%s/%s)", s.Type, dir, strings.TrimPrefix(s.Type, "stripe_"))
	in.Guides = nil
	m := &page{markdown: &markdown{}, in: in, resource: dir == "resources"}
	lead := fmt.Sprintf("The `%s` attribute of the %s %s, on a page of its own to keep that page within the Registry's size limit.", a.Name, link, words)
	m.add(frontMatter(fmt.Sprintf("%s %s: %s", s.Type, words, a.Name), lead))
	m.add(fmt.Sprintf("# %s %s: `%s`", s.Type, words, a.Name))
	m.add(lead)
	m.subsections([]*attr{a}, "", a.argument())
	return m.String()
}

// everyArgumentHasADefault makes the default check total: every optional argument of a
// resource, at any depth, states its default. Until the evidence of v0.1.0 is pinned (ADR
// 2026-10-29-full-attribute-coverage, Step 6), only the provider's own arguments and those the
// pinned evidence verifies, and those only an update takes, must; the coverage report counts
// the rest per table.
const everyArgumentHasADefault = false

// checkDefaults fails on an optional argument without a default statement where one must exist,
// naming the page and the path of each (D4). An argument whose value is a nested object with
// members states its members' defaults, not its own; the timeouts state theirs in their section.
func checkDefaults(s *schemas, total bool) error {
	var missing []string
	var walk func(page string, attrs []*attr, prefix string, must func(*attr) bool)
	walk = func(page string, attrs []*attr, prefix string, must func(*attr) bool) {
		for _, a := range attrs {
			if !a.argument() || prefix == "" && a.Name == "timeouts" {
				continue
			}
			path := prefix + a.Name
			leaf := a.Nesting == nestNone || len(a.Children) == 0
			if a.Optional && leaf && a.Default == "" && must(a) {
				missing = append(missing, page+": "+path)
			}
			walk(page, a.Children, path+".", must)
		}
	}
	walk("index.md", s.Provider.Attrs, "", func(*attr) bool { return true })
	for _, r := range s.Resources {
		walk(pagePath("resources", r.Type), r.Attrs, "", func(a *attr) bool { return total || a.NeedsDefault })
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("optional arguments without a default statement (ADR 2026-10-29-full-attribute-coverage, D4):\n  %s", strings.Join(missing, "\n  "))
	}
	return nil
}
