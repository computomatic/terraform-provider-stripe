package engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
)

// node is a table field with every decision of the D2 rules resolved.
type node struct {
	fields.Field
	// Path is the dotted Stripe field path.
	Path string
	Kids []*node

	Required bool
	Optional bool
	Computed bool
	// Sensitive hides the value in plans and masks it in logs.
	Sensitive bool
	// Replace attaches RequiresReplace: Stripe rejects the field on update.
	Replace bool
	// UseState attaches UseStateForUnknown: Stripe never changes the value by itself.
	UseState bool
	// Once is a once-returned output: a response that omits it keeps the prior value.
	Once bool
	// Preserve keeps the configured value when the response redacts or reformats it.
	Preserve bool
}

// computedOnly reports an output the user cannot set.
func (n *node) computedOnly() bool { return n.Computed && !n.Optional && !n.Required }

// model is a declaration compiled against its field table. It is read-only once built and
// shared by the resource and the data source.
type model struct {
	decl  Declaration
	nodes []*node
	// sensitiveNames are the field names masked in request logs.
	sensitiveNames []string
}

// reservedNames are the root attribute names Terraform does not allow.
var reservedNames = map[string]bool{
	"count": true, "depends_on": true, "for_each": true, "lifecycle": true, "provider": true, "providers": true,
}

type pathSet map[string]bool

func newPathSet(paths []string) pathSet {
	s := pathSet{}
	for _, p := range paths {
		s[p] = true
	}
	return s
}

// compile resolves the declaration against the table and rejects any declaration entry
// that names no field, so a stale declaration fails when the schema is built.
func compile(decl Declaration, table []fields.Field) (*model, error) {
	if decl.TypeName == "" {
		return nil, fmt.Errorf("declaration has no type name")
	}
	if decl.Paths.Retrieve == "" {
		return nil, fmt.Errorf("%s: declaration has no retrieve path", decl.TypeName)
	}
	if decl.Delete != NoDelete && decl.Paths.Create == "" {
		return nil, fmt.Errorf("%s: declaration has no create path", decl.TypeName)
	}
	switch decl.Delete {
	case Delete:
		if decl.Paths.Delete == "" {
			return nil, fmt.Errorf("%s: the delete strategy needs a delete path", decl.TypeName)
		}
	case Archive:
		if decl.Archive == nil || decl.Paths.Update == "" {
			return nil, fmt.Errorf("%s: the archive strategy needs an archive action and an update path", decl.TypeName)
		}
	case Expire:
		if decl.Expire == nil || decl.Paths.Update == "" {
			return nil, fmt.Errorf("%s: the expire strategy needs an expire action and an update path", decl.TypeName)
		}
	}

	c := compiler{
		decl:         decl,
		once:         newPathSet(decl.OnceReturned),
		unDefaulted:  newPathSet(decl.NotServerDefaulted),
		sensitive:    newPathSet(decl.Sensitive),
		preserve:     newPathSet(decl.PreserveConfig),
		sets:         newPathSet(decl.Sets),
		used:         pathSet{},
		sensitiveSet: pathSet{},
	}
	nodes, err := c.build(table, "", true, false, false)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", decl.TypeName, err)
	}
	if err := c.unused(); err != nil {
		return nil, fmt.Errorf("%s: %w", decl.TypeName, err)
	}

	m := &model{decl: decl, nodes: nodes}
	if _, ok := m.top("id"); !ok {
		return nil, fmt.Errorf("%s: the table has no `id` field", decl.TypeName)
	}
	for _, n := range nodes {
		if reservedNames[n.AttrName()] {
			return nil, fmt.Errorf("%s: field `%s` is a name Terraform reserves; add a rename to the declaration", decl.TypeName, n.AttrName())
		}
		if n.AttrName() == "timeouts" || n.AttrName() == decl.Parent {
			return nil, fmt.Errorf("%s: field `%s` collides with a provider-added attribute; rename it", decl.TypeName, n.AttrName())
		}
	}
	for _, l := range decl.Lookups {
		n, ok := m.top(l.Field)
		if !ok || n.Shape != fields.String || !n.Response {
			return nil, fmt.Errorf("%s: lookup key `%s` is not a top-level string field the response returns", decl.TypeName, l.Field)
		}
		if decl.Paths.List == "" {
			return nil, fmt.Errorf("%s: lookup keys need a list path", decl.TypeName)
		}
	}

	names := map[string]bool{}
	for p := range c.sensitiveSet {
		names[p[strings.LastIndex(p, ".")+1:]] = true
	}
	for name := range names {
		m.sensitiveNames = append(m.sensitiveNames, name)
	}
	sort.Strings(m.sensitiveNames)
	return m, nil
}

// top returns the top-level node with the given Stripe field name.
func (m *model) top(name string) (*node, bool) {
	for _, n := range m.nodes {
		if n.Name == name {
			return n, true
		}
	}
	return nil, false
}

type compiler struct {
	decl         Declaration
	once         pathSet
	unDefaulted  pathSet
	sensitive    pathSet
	preserve     pathSet
	sets         pathSet
	used         pathSet
	sensitiveSet pathSet
}

func (c *compiler) mark(kind string, set pathSet, path string) bool {
	if set[path] {
		c.used[kind+"/"+path] = true
		return true
	}
	return false
}

// unused reports a declaration entry that matched no field.
func (c *compiler) unused() error {
	for _, e := range []struct {
		kind string
		set  pathSet
	}{
		{"once_returned", c.once},
		{"not_server_defaulted", c.unDefaulted},
		{"sensitive", c.sensitive},
		{"preserve_config", c.preserve},
		{"sets", c.sets},
	} {
		for p := range e.set {
			if !c.used[e.kind+"/"+p] {
				return fmt.Errorf("%s entry `%s` names no field", e.kind, p)
			}
		}
	}
	for p := range c.decl.Rename {
		if !c.used["rename/"+p] {
			return fmt.Errorf("rename entry `%s` names no field", p)
		}
	}
	return nil
}

// build resolves one level of the table. parentOutput is true when every field below is a
// Computed output; parentReplace when an enclosing field already forces replacement.
func (c *compiler) build(table []fields.Field, prefix string, top, parentOutput, parentReplace bool) ([]*node, error) {
	var out []*node
	for _, f := range table {
		path := prefix + f.Name
		if attrName, ok := c.decl.Rename[path]; ok {
			f.Attr = attrName
			c.used["rename/"+path] = true
		}
		if c.mark("sets", c.sets, path) {
			if f.Shape != fields.ScalarList {
				return nil, fmt.Errorf("sets entry `%s` is not a scalar array", path)
			}
			f.Shape = fields.ScalarSet
		}
		n := &node{Field: f, Path: path}

		once := c.mark("once_returned", c.once, path)
		n.Sensitive = c.mark("sensitive", c.sensitive, path) || once
		n.Preserve = c.mark("preserve_config", c.preserve, path)
		notDefaulted := c.mark("not_server_defaulted", c.unDefaulted, path)
		if n.Sensitive {
			c.sensitiveSet[path] = true
		}
		if n.Preserve && !f.Writable() {
			return nil, fmt.Errorf("preserve_config entry `%s` is not a writable field", path)
		}

		switch {
		case once:
			n.Once, n.Computed = true, true
		case !f.Writable() || parentOutput:
			n.Computed = true
		default:
			n.Required = f.Required && f.Create
			n.Optional = !n.Required
			n.Computed = !n.Required &&
				((f.ServerDefaulted && !notDefaulted) || (f.Response && !f.Create))
			if top && f.Name == "metadata" && f.Shape == fields.StringMap {
				n.Computed = false
			}
		}
		n.Replace = f.CreateOnly() && !parentReplace && !n.computedOnly()
		n.UseState = n.Computed && !n.Required &&
			((top && (f.Name == "id" || f.Name == "created" || f.Name == "livemode")) || f.CreateOnly())

		switch f.Shape {
		case fields.Object, fields.ObjectList, fields.ObjectMap:
			kids, err := c.build(f.Fields, path+".", false, n.computedOnly(), parentReplace || n.Replace)
			if err != nil {
				return nil, err
			}
			n.Kids = kids
		}
		out = append(out, n)
	}
	return out, nil
}

// attrType returns the framework type of the attribute built from n.
func attrType(n *node) attr.Type {
	switch n.Shape {
	case fields.Bool:
		return types.BoolType
	case fields.Int:
		return types.Int64Type
	case fields.Float:
		return types.Float64Type
	case fields.Object:
		return objectType(n.Kids)
	case fields.ObjectList:
		return types.ListType{ElemType: objectType(n.Kids)}
	case fields.ObjectMap:
		return types.MapType{ElemType: objectType(n.Kids)}
	case fields.StringMap:
		return types.MapType{ElemType: types.StringType}
	case fields.ScalarList:
		return types.ListType{ElemType: scalarType(n.Elem)}
	case fields.ScalarSet:
		return types.SetType{ElemType: scalarType(n.Elem)}
	default:
		return types.StringType
	}
}

func scalarType(s fields.Shape) attr.Type {
	switch s {
	case fields.Bool:
		return types.BoolType
	case fields.Int:
		return types.Int64Type
	case fields.Float:
		return types.Float64Type
	default:
		return types.StringType
	}
}

func objectType(kids []*node) types.ObjectType {
	attrs := make(map[string]attr.Type, len(kids))
	for _, k := range kids {
		attrs[k.AttrName()] = attrType(k)
	}
	return types.ObjectType{AttrTypes: attrs}
}
