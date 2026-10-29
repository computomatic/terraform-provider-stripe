package engine

// AttributeDoc is what an attribute's page says besides its schema description (ADR
// 2026-10-29-full-attribute-coverage, D3 and D4). tools/docgen renders it.
type AttributeDoc struct {
	// Default is an optional argument's default statement, or "" where none is recorded yet.
	Default string
	// NeedsDefault says the page must state a default: the pinned evidence verifies the
	// argument, or only an update takes it, which the provider never sends on create.
	NeedsDefault bool
	// Copy is, on an embedded copy of another Stripe object, the data source type that reads it.
	Copy string
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
				doc.NeedsDefault = n.Verified || n.Update && !n.Create
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
