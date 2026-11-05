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
// line, which splits again while it is over budget (guidePages). It returns the page and the
// guides, keyed by their paths under docs/.
func fit(s *schema, dir string, in pageInput, render func(*schema, pageInput) (string, error), budget int) (map[string]string, error) {
	out := map[string]string{}
	page, err := render(s, in)
	if err != nil {
		return nil, err
	}
	if len(page) > budget {
		in.Guides = map[string]string{}
		blocks := map[string]map[string]string{}
		for _, a := range s.Attrs {
			if a.Nesting != nestNone && a.Name != "timeouts" {
				if blocks[a.Name], err = guidePages(s, dir, a, a.Name, a.argument(), in, budget); err != nil {
					return nil, err
				}
			}
		}
		size := func(name string) int { return len(blocks[name][guidePath(s, dir, name)]) }
		names := slices.Collect(maps.Keys(blocks))
		sort.Slice(names, func(i, j int) bool {
			if size(names[i]) != size(names[j]) {
				return size(names[i]) > size(names[j])
			}
			return names[i] < names[j]
		})
		for _, name := range names {
			if len(page) <= budget {
				break
			}
			in.Guides[name] = guideName(s, dir, name)
			maps.Copy(out, blocks[name])
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

// guidePages renders the guide page of the nested block a at path, keyed by its path under docs/
// with the guides it needs: while the page is over budget, the largest nested block directly
// below a moves to a guide page of its own, which splits the same way.
func guidePages(s *schema, dir string, a *attr, path string, labelled bool, in pageInput, budget int) (map[string]string, error) {
	in.Guides = map[string]string{}
	out := map[string]string{}
	page := renderGuide(s, dir, a, path, labelled, in)
	if len(page) > budget {
		blocks := map[string]map[string]string{}
		for _, c := range a.Children {
			if c.Nesting == nestNone {
				continue
			}
			var err error
			if blocks[path+"."+c.Name], err = guidePages(s, dir, c, path+"."+c.Name, labelled && a.argument(), in, budget); err != nil {
				return nil, err
			}
		}
		size := func(p string) int { return len(blocks[p][guidePath(s, dir, p)]) }
		paths := slices.Collect(maps.Keys(blocks))
		sort.Slice(paths, func(i, j int) bool {
			if size(paths[i]) != size(paths[j]) {
				return size(paths[i]) > size(paths[j])
			}
			return paths[i] < paths[j]
		})
		for _, p := range paths {
			if len(page) <= budget {
				break
			}
			in.Guides[p] = guideName(s, dir, p)
			maps.Copy(out, blocks[p])
			page = renderGuide(s, dir, a, path, labelled, in)
		}
		if len(page) > budget {
			return nil, fmt.Errorf("%s is %d bytes with every nested block on a guide page, over the budget of %d", guidePath(s, dir, path), len(page), budget)
		}
	}
	out[guidePath(s, dir, path)] = page
	return out, nil
}

func guideName(s *schema, dir, attrName string) string {
	return strings.TrimPrefix(s.Type, "stripe_") + "-" + kindSlug[dir] + "-" + attrName
}

func guidePath(s *schema, dir, attrPath string) string {
	return filepath.Join("guides", guideName(s, dir, attrPath)+".md")
}

// renderGuide is the guide page of one nested block of a page, at path: its subsections, each
// line as the page would write it, the default statements included. labelled says whether the
// block's parent is set by the user.
func renderGuide(s *schema, dir string, a *attr, path string, labelled bool, in pageInput) string {
	kind := kindSlug[dir]
	words := strings.ReplaceAll(kind, "-", " ")
	link := fmt.Sprintf("[`%s`](../%s/%s)", s.Type, dir, strings.TrimPrefix(s.Type, "stripe_"))
	m := &page{markdown: &markdown{}, in: in, resource: dir == "resources"}
	lead := fmt.Sprintf("The `%s` attribute of the %s %s, on a page of its own to keep that page within the Registry's size limit.", path, link, words)
	m.add(frontMatter(fmt.Sprintf("%s %s: %s", s.Type, words, path), lead))
	m.add(fmt.Sprintf("# %s %s: `%s`", s.Type, words, path))
	m.add(lead)
	m.subsections([]*attr{a}, strings.TrimSuffix(path, a.Name), labelled)
	return m.String()
}

// everyArgumentHasADefault makes the default check total: every optional argument of a
// resource, at any depth, states its default. Until the evidence of v0.1.0 is pinned (ADR
// 2026-10-29-full-attribute-coverage, Step 6), only the provider's own arguments and those only
// an update takes must: evidence arrives object by object, so a verified argument whose default
// is not recorded yet does not fail the docs. The coverage report names those per table and
// counts the unverified rest.
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
