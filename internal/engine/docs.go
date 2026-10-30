package engine

// AttributeDoc is what an attribute's page says besides its schema description (ADR
// 2026-10-29-full-attribute-coverage, D3 and D4). tools/docgen renders it.
type AttributeDoc struct {
	// Default is an optional argument's default statement, or "" where none is recorded yet.
	Default string
	// NeedsDefault says the page must state a default: only an update takes the argument, which
	// the provider never sends on create.
	NeedsDefault bool
	// Removal is, on an Optional and Computed argument, the sentence that says what removing it
	// from the configuration does, which follows the default statement.
	Removal string
	// Copy is, on an embedded copy of another Stripe object, the data source type that reads it.
	Copy string
}

// The removal sentences (ADR 2026-10-29-full-attribute-coverage, D4, E2 and E5). An argument the
// plan keeps (UseStateForUnknown) leaves the current value; one in a list element, which the
// plan cannot keep, Stripe sets when the list is sent again; any other the provider stops sending,
// and the state takes Stripe's value at the next apply.
const (
	RemovalKeeps   = "Removing it from the configuration leaves the current value."
	RemovalInList  = "Removing it from the configuration lets Stripe set it when the list is next sent."
	RemovalNotSent = "Removing it from the configuration stops sending it, and the next apply records the value Stripe holds."
)

// removal is the removal sentence of an Optional and Computed argument, from what the plan does
// with it.
func removal(n *node) string {
	switch {
	case !n.Optional || !n.Computed:
		return ""
	case n.UseState:
		return RemovalKeeps
	case n.InList:
		return RemovalInList
	}
	return RemovalNotSent
}

// AttributeDocs maps the dotted attribute path of every attribute whose page says more than its
// description to what it says.
func (r *Resource) AttributeDocs() map[string]AttributeDoc { return r.m.attributeDocs(true) }

// AttributeDocs maps the dotted attribute path of every embedded copy to the data source type
// that reads the copied object. A data source has no arguments with defaults.
func (d *DataSource) AttributeDocs() map[string]AttributeDoc { return d.m.attributeDocs(false) }

func (m *model) attributeDocs(arguments bool) map[string]AttributeDoc {
	out := map[string]AttributeDoc{}
	var walk func([]*node, string)
	walk = func(nodes []*node, prefix string) {
		for _, n := range nodes {
			path := prefix + n.AttrName()
			var doc AttributeDoc
			if arguments && n.Optional {
				if n.Default != nil {
					doc.Default = n.Default.Statement()
				}
				doc.NeedsDefault = n.Update && !n.Create
				doc.Removal = removal(n)
			}
			doc.Copy = n.Copy
			if doc != (AttributeDoc{}) {
				out[path] = doc
			}
			walk(n.Kids, path+".")
		}
	}
	walk(m.nodes, "")
	return out
}
