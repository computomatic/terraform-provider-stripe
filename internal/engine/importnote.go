package engine

import (
	"fmt"
	"slices"
	"strings"
)

// importNote is the Import section's note: the declaration's, then a sentence that lists the
// arguments Stripe never returns, which an import cannot recover (ADR
// 2026-10-29-full-attribute-coverage, E3), leaving out those the declaration's note already
// names. A configuration that sets one plans an update after the import, or a replacement where
// the argument cannot change in place, which flags the note as a warning. A kept member of a
// keyed list's elements whose response form a read can see (listedKept) is adopted where Stripe
// lists some and replaces where it lists none; any other such member replaces
// (requiresReplaceForKeptInKeyed).
func (m *model) importNote() (string, NoteLevel) {
	note, level := strings.TrimSpace(m.decl.ImportNote), m.decl.ImportNoteLevel
	var update, replace, adopt []string
	var walk func(nodes []*node, prefix string, replacing bool)
	walk = func(nodes []*node, prefix string, replacing bool) {
		for _, n := range nodes {
			name := prefix + n.AttrName()
			// A kept member of a keyed list's elements replaces on a null state, as after an
			// import, unless a read can see it (listedKept, below).
			replaces := replacing || n.Replace || n.ReplaceIfSet || n.Fixed || m.keptInKeyed(n)
			// A list of discounts the resource maps back by key is read back too.
			if n.computedOnly() || n == m.discounts || slices.Contains(m.lineDiscounts, n) {
				continue
			}
			if n.Response || n.Once {
				walk(n.Kids, name+".", replaces)
				continue
			}
			if named(note, name) {
				continue
			}
			if slices.Contains(m.listedKept(), n) {
				adopt = append(adopt, "`"+name+"`")
				continue
			}
			if replaces {
				replace = append(replace, "`"+name+"`")
			} else {
				update = append(update, "`"+name+"`")
			}
		}
	}
	walk(m.nodes, "", false)
	if len(update)+len(replace)+len(adopt) == 0 {
		return note, level
	}
	all := slices.Concat(update, replace, adopt)
	thing := Noun(m.decl.TypeName)
	generated := fmt.Sprintf("Stripe does not return %s, so an imported %s has %s null", inWords(all, "or"), thing, pronoun(len(all)))
	switch {
	case len(update)+len(replace) == 0:
		generated += "."
	case len(replace) == 0:
		generated += ": a configuration that sets " + oneOf(len(update)) + " plans an update after the import."
	case len(update) == 0:
		generated += ": a configuration that sets " + oneOf(len(replace)) + " plans a replacement after the import."
		level = NoteWarning
	default:
		generated += fmt.Sprintf(": a configuration that sets %s plans an update after the import, and one that sets %s plans a replacement.", inWords(update, "or"), inWords(replace, "or"))
		level = NoteWarning
	}
	if len(adopt) > 0 {
		generated += fmt.Sprintf(" The import reads whether each element has any %s: setting %s on an element that has none replaces the %s, and on one that has some, whose form the provider cannot read, takes the configured value with nothing sent; changing it later replaces the %s, and so does a configured value not known until apply.", inWords(adopt, "or"), pronoun(len(adopt)), thing, thing)
	}
	if note == "" {
		return generated, level
	}
	return note + " " + generated, level
}

// named reports whether a note names the attribute at a dotted path, or one it is inside, in
// backticks.
func named(note, name string) bool {
	for p := name; ; {
		if strings.Contains(note, "`"+p+"`") {
			return true
		}
		i := strings.LastIndex(p, ".")
		if i < 0 {
			return false
		}
		p = p[:i]
	}
}

// inWords joins items as a sentence does: `a`, `a or b`, `a, b or c`.
func inWords(items []string, conj string) string {
	if len(items) == 1 {
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " " + conj + " " + items[len(items)-1]
}

func pronoun(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}

func oneOf(n int) string {
	if n == 1 {
		return "it"
	}
	return "one"
}
