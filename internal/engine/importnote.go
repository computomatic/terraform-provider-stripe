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
// the argument cannot change in place, which flags the note as a warning; on a resource whose
// destroy is refused that replacement fails the plan, and the note says so. One that sets a
// RefuseChange argument fails the plan, a warning too, and the note says to ignore its changes.
// A kept member of a keyed list's elements whose response form a read can see (listedKept) is
// adopted where Stripe lists some and replaces where it lists none; any other such member
// replaces (requiresReplaceForKeptInKeyed).
func (m *model) importNote() (string, NoteLevel) {
	note, level := strings.TrimSpace(m.decl.ImportNote), m.decl.ImportNoteLevel
	var update, replace, refuse, adopt []string
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
			switch {
			// A SetOnce entry is no refusal here: a plan may set it where the state holds none.
			case refusedAt(m.decl, n.Path):
				refuse = append(refuse, "`"+name+"`")
			case replaces:
				replace = append(replace, "`"+name+"`")
			default:
				update = append(update, "`"+name+"`")
			}
		}
	}
	walk(m.nodes, "", false)
	if len(update)+len(replace)+len(refuse)+len(adopt) == 0 {
		return note, level
	}
	all := slices.Concat(update, replace, refuse, adopt)
	thing := Noun(m.decl.TypeName)
	generated := fmt.Sprintf("Stripe does not return %s, so an imported %s has %s null", inWords(all, "or"), thing, pronoun(len(all)))
	// A resource whose destroy is refused fails the plan of a replacement instead.
	replaces, because := "plans a replacement", ""
	if m.decl.Delete == Refuse {
		replaces, because = "fails the plan", ", because this resource cannot be replaced"
	}
	// What a configuration that sets each group plans after the import, in one sentence: a lone
	// group calls its arguments `it` or `one`, as the note names them just before.
	type group struct {
		names         []string
		does, because string
	}
	var groups []group
	for _, g := range []group{
		{update, "plans an update", ""},
		{replace, replaces, because},
		{refuse, "fails the plan", ", because the provider refuses a change of it: add it to `lifecycle { ignore_changes }`"},
	} {
		if len(g.names) > 0 {
			groups = append(groups, g)
		}
	}
	for i, g := range groups {
		sets := inWords(g.names, "or")
		switch {
		case i == 0 && len(groups) == 1:
			generated += ": a configuration that sets " + oneOf(len(g.names)) + " " + g.does + " after the import" + g.because
		case i == 0:
			generated += ": a configuration that sets " + sets + " " + g.does + " after the import" + g.because
		case i == len(groups)-1:
			generated += ", and one that sets " + sets + " " + g.does + g.because
		default:
			generated += ", one that sets " + sets + " " + g.does + g.because
		}
	}
	generated += "."
	if len(replace)+len(refuse) > 0 {
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
