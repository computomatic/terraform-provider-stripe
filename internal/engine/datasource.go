package engine

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// NewDataSource compiles the declaration against its table and returns the data source
// constructor the provider registers.
func NewDataSource(decl Declaration, table []fields.Field) (func() datasource.DataSource, error) {
	m, err := compile(decl, fields.ForDataSource(table))
	if err != nil {
		return nil, err
	}
	s := m.dataSourceSchema()
	objType, ok := s.Type().(basetypes.ObjectType)
	if !ok {
		return nil, fmt.Errorf("%s: schema type is %T, not an object", decl.TypeName, s.Type())
	}
	return func() datasource.DataSource { return &DataSource{m: m, schema: s, objType: objType} }, nil
}

// DataSource is the generic data source: it reads one object by ID or by a unique key.
type DataSource struct {
	m       *model
	schema  dschema.Schema
	objType basetypes.ObjectType
	api     api
}

var (
	_ datasource.DataSourceWithConfigure      = (*DataSource)(nil)
	_ datasource.DataSourceWithValidateConfig = (*DataSource)(nil)
)

func (d *DataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = d.m.decl.TypeName
}

func (d *DataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = d.schema
}

func (d *DataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	data, ok := req.ProviderData.(*ProviderData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("expected the provider's data, got %T", req.ProviderData))
		return
	}
	d.api = api{m: d.m, client: data.Client, typeAt: func(p path.Path) (attr.Type, bool) {
		a, diags := d.schema.AttributeAtPath(ctx, p)
		if diags.HasError() {
			return nil, false
		}
		return a.GetType(), true
	}}
}

// ListParamNames lists the attributes of the declaration's list parameters, which are optional
// arguments but no lookup keys. tools/docgen documents them apart from the keys.
func (d *DataSource) ListParamNames() []string {
	var names []string
	for _, p := range d.m.decl.ListParams {
		names = append(names, d.attrName(p.Name))
	}
	return names
}

// lookupNames lists the attributes a block may use to find its object, `id` first.
func (d *DataSource) lookupNames() []string {
	if d.m.decl.Singleton {
		return nil
	}
	names := []string{"id"}
	for _, l := range d.m.decl.Lookups {
		names = append(names, d.attrName(l.Field))
	}
	return names
}

func (d *DataSource) attrName(field string) string {
	n, _ := d.m.top(field)
	return n.AttrName()
}

// ValidateConfig requires exactly one lookup form per block, except for a singleton and an
// object found by parameters, which have none. Unknown values are left for apply time.
func (d *DataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var config basetypes.ObjectValue
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d.m.validateLowerCase(ctx, req.Config, &resp.Diagnostics)
	if d.m.decl.Singleton || len(d.m.decl.FindBy) > 0 {
		return
	}
	set := 0
	for _, name := range d.lookupNames() {
		v := config.Attributes()[name]
		if v.IsUnknown() {
			return
		}
		if !v.IsNull() {
			set++
		}
	}
	if set != 1 {
		resp.Diagnostics.AddError("Invalid lookup", fmt.Sprintf("exactly one of %s must be set, got %d", strings.Join(d.lookupNames(), ", "), set))
	}
}

func (d *DataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config basetypes.ObjectValue
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, ReadTimeout)
	defer cancel()

	id, parent := d.m.ids(config)
	var body wire.Object
	var err error
	if d.m.decl.Singleton {
		body, err = d.api.call(ctx, "GET", d.m.decl.Paths.Retrieve, d.api.expanded(nil, ""))
	} else if len(d.m.decl.FindBy) > 0 {
		body, err = d.findBy(ctx, config, parent, resp)
	} else if id != "" && d.m.decl.FindInList {
		var params wire.Object
		params, err = d.listParams(ctx, config, parent, resp)
		if err == nil && params == nil {
			return
		}
		if err == nil {
			body, err = d.find(ctx, id, parent, params)
		}
		if err == nil && body == nil {
			resp.Diagnostics.AddError("Not found", fmt.Sprintf("no %s found for id=%s", d.m.decl.TypeName, id))
			return
		}
	} else if id != "" {
		body, err = d.api.call(ctx, "GET", d.m.expand(d.m.decl.Paths.Retrieve, id, parent), d.api.retrieved(nil))
		if d.m.gone(err, &resp.Diagnostics) || err == nil && isDeletedStub(body) {
			resp.Diagnostics.AddError("Not found", fmt.Sprintf("no %s found for id=%s", d.m.decl.TypeName, id))
			return
		}
	} else {
		body, err = d.lookup(ctx, config, parent, resp)
	}
	if err != nil {
		d.api.fail(&resp.Diagnostics, err)
		return
	}
	if body == nil {
		return
	}
	state, diags := d.m.decodeState(body, config, d.objType, true, nil)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// hasMore reports whether a list has another page: `has_more` on `/v1`, a non-null
// `next_page_url` on `/v2`, which has no `has_more`.
func hasMore(page wire.Object, v2 bool) bool {
	if !v2 {
		more, _ := page.GetBool("has_more")
		return more
	}
	next, ok := page.Get("next_page_url")
	if !ok {
		return false
	}
	_, isNull := next.(wire.Null)
	return !isNull
}

// maxFindPages caps the pages find reads, whatever the list answers.
const maxFindPages = 200

// listParams returns the declaration's list parameters with their configured values, and with
// the parent's for those left unset. A nil result with no error means an error diagnostic was
// added.
func (d *DataSource) listParams(ctx context.Context, config basetypes.ObjectValue, parent string, resp *datasource.ReadResponse) (wire.Object, error) {
	params := wire.Object{}
	var parentBody wire.Object
	for _, p := range d.m.decl.ListParams {
		value := stringAttr(config, d.attrName(p.Name))
		if value == "" {
			if parentBody == nil {
				body, err := d.api.call(ctx, "GET", d.m.expand(p.ParentPath, "", parent), nil)
				if err != nil {
					return nil, err
				}
				parentBody = body
			}
			value = idOf(parentBody, p.Name)
		}
		if value == "" {
			resp.Diagnostics.AddError("No "+p.Name, fmt.Sprintf("%s %s has no `%s`: %s", d.m.decl.Parent, parent, p.Name, p.Unset))
			return nil, nil
		}
		params = append(params, wire.Field{Name: p.Name, Value: wire.String(value)})
	}
	return params, nil
}

// idOf returns the ID a field of body holds, as a string or as an expanded object, or "" when
// it holds none.
func idOf(body wire.Object, field string) string {
	v, _ := body.Get(field)
	switch v := v.(type) {
	case wire.String:
		return string(v)
	case wire.Object:
		id, _ := v.GetString("id")
		return id
	}
	return ""
}

// find reads the parent's list page by page, sending params with each, and returns the element
// with the ID, or nil when no page holds it. A page with more to come that lists no ID not seen before means the list
// is not moving on, which Stripe has been seen to do for some lists, so it ends the search
// with an error instead of looping.
func (d *DataSource) find(ctx context.Context, id, parent string, listParams wire.Object) (wire.Object, error) {
	after := ""
	seen := map[string]bool{}
	for pages := 0; pages < maxFindPages; pages++ {
		params := append(wire.Object{{Name: "limit", Value: wire.Int(listLimit)}}, listParams...)
		if after != "" {
			params = append(params, wire.Field{Name: "starting_after", Value: wire.String(after)})
		}
		page, err := d.api.call(ctx, "GET", d.m.expand(d.m.decl.Paths.List, "", parent), d.api.expanded(params, "data."))
		if err != nil {
			return nil, err
		}
		data, _ := page.Get("data")
		items, _ := data.(wire.Array)
		last, fresh := "", false
		for _, item := range items {
			obj, ok := item.(wire.Object)
			if !ok {
				return nil, fmt.Errorf("list item is not an object")
			}
			got, _ := obj.GetString("id")
			if got == id {
				return obj, nil
			}
			if !seen[got] {
				seen[got], fresh = true, true
			}
			last = got
		}
		if !hasMore(page, false) || last == "" {
			return nil, nil
		}
		if !fresh {
			return nil, fmt.Errorf("the list of %s did not move past %s", d.m.decl.TypeName, after)
		}
		after = last
	}
	return nil, fmt.Errorf("the list of %s has more than %d pages", d.m.decl.TypeName, maxFindPages)
}

// lookup finds the one object whose unique key matches the configured value, using the
// list endpoint's filter (never Search, which lags writes). It reads one page, which is
// enough to prove zero, one or more than one match. A nil body with no error means an
// error diagnostic was added.
func (d *DataSource) lookup(ctx context.Context, config basetypes.ObjectValue, parent string, resp *datasource.ReadResponse) (wire.Object, error) {
	for _, l := range d.m.decl.Lookups {
		value := stringAttr(config, d.attrName(l.Field))
		if value == "" {
			continue
		}
		param := l.Param
		if param == "" {
			param = l.Field
		}
		var filter wire.Value = wire.String(value)
		if l.Array {
			filter = wire.Array{wire.String(value)}
		}
		limit := wire.Int(listLimit)
		if d.m.v2 {
			limit = listLimitV2
		}
		params := wire.Object{
			{Name: "limit", Value: limit},
			{Name: param, Value: filter},
		}
		page, err := d.api.call(ctx, "GET", d.m.expand(d.m.decl.Paths.List, "", parent), d.api.expanded(params, "data."))
		if err != nil {
			return nil, err
		}
		return d.only(page, fmt.Sprintf("%s=%s", l.Field, value), "; use `id`", resp), nil
	}
	resp.Diagnostics.AddError("Invalid lookup", "no lookup value is set")
	return nil, nil
}

// only returns the one object a list page holds. Zero objects, or more than one, adds an error
// diagnostic that names label and returns nil. hint ends the error for more than one.
func (d *DataSource) only(page wire.Object, label, hint string, resp *datasource.ReadResponse) wire.Object {
	data, _ := page.Get("data")
	items, _ := data.(wire.Array)
	more := hasMore(page, d.m.v2)
	switch {
	case len(items) == 0:
		resp.Diagnostics.AddError("Not found", fmt.Sprintf("no %s found for %s", d.m.decl.TypeName, label))
	case len(items) > 1 || more:
		count := strconv.Itoa(len(items))
		if more {
			count = "more than " + count
		}
		resp.Diagnostics.AddError("Ambiguous lookup", fmt.Sprintf("%s matches %s objects for %s%s", d.m.decl.TypeName, count, label, hint))
	default:
		if obj, ok := items[0].(wire.Object); ok {
			return obj
		}
		resp.Diagnostics.AddError("Unexpected Stripe response", "list item is not an object")
	}
	return nil
}

// findBy reads the object by the declaration's find parameters, sent with their configured
// values: from the retrieve path, where a 404 is not found, or from the list path, which must
// list exactly one. A nil body with no error means an error diagnostic was added.
func (d *DataSource) findBy(ctx context.Context, config basetypes.ObjectValue, parent string, resp *datasource.ReadResponse) (wire.Object, error) {
	var params wire.Object
	var labels []string
	for _, p := range d.m.decl.FindBy {
		n, ok := d.m.top(p.Name)
		if !ok {
			n = &node{Field: fields.Field{Name: p.Name, Shape: fields.String}}
		}
		value, set, err := encodeValue(n, config.Attributes()[n.AttrName()], func(*node) bool { return true })
		if err != nil {
			return nil, err
		}
		if !set {
			continue
		}
		params = append(params, wire.Field{Name: p.Name, Value: value})
		if err := d.m.refuseEmptyObjects([]*node{n}, params[len(params)-1:], opLookup); err != nil {
			return nil, err
		}
		labels = appendLabels(labels, p.Name, value)
	}
	label := strings.Join(labels, ", ")
	if d.m.decl.Paths.Retrieve != "" {
		body, err := d.api.call(ctx, "GET", d.m.expand(d.m.decl.Paths.Retrieve, "", parent), d.api.expanded(params, ""))
		if d.m.gone(err, &resp.Diagnostics) {
			resp.Diagnostics.AddError("Not found", fmt.Sprintf("no %s found for %s", d.m.decl.TypeName, label))
			return nil, nil
		}
		return body, err
	}
	limit := wire.Int(listLimit)
	if d.m.v2 {
		limit = listLimitV2
	}
	params = append(params, wire.Field{Name: "limit", Value: limit})
	page, err := d.api.call(ctx, "GET", d.m.expand(d.m.decl.Paths.List, "", parent), d.api.expanded(params, "data."))
	if err != nil {
		return nil, err
	}
	return d.only(page, label, "", resp), nil
}

// appendLabels adds `name=value` for a scalar, and one `name.field=value` for each field of an
// object, to labels.
func appendLabels(labels []string, name string, value wire.Value) []string {
	switch v := value.(type) {
	case wire.Object:
		for _, f := range v {
			labels = appendLabels(labels, name+"."+f.Name, f.Value)
		}
		return labels
	case wire.Int:
		return append(labels, name+"="+strconv.FormatInt(int64(v), 10))
	default:
		s, _ := v.(wire.String)
		return append(labels, name+"="+string(s))
	}
}
