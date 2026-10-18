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
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// NewDataSource compiles the declaration against its table and returns the data source
// constructor the provider registers.
func NewDataSource(decl Declaration, table []fields.Field) (func() datasource.DataSource, error) {
	m, err := compile(decl, table)
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
	client, ok := req.ProviderData.(Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("expected a Stripe client, got %T", req.ProviderData))
		return
	}
	d.api = api{m: d.m, client: client, typeAt: func(p path.Path) (attr.Type, bool) {
		a, diags := d.schema.AttributeAtPath(ctx, p)
		if diags.HasError() {
			return nil, false
		}
		return a.GetType(), true
	}}
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

// ValidateConfig requires exactly one lookup form per block. Unknown values are left for
// apply time.
func (d *DataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var config basetypes.ObjectValue
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.m.decl.Singleton {
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
	} else if id != "" {
		body, err = d.api.call(ctx, "GET", d.m.expand(d.m.decl.Paths.Retrieve, id, parent), d.api.expanded(nil, ""))
		if stripeapi.IsNotFound(err) || err == nil && isDeletedStub(body) {
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
	state, diags := d.m.decodeState(body, config, d.objType, true)
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
		data, _ := page.Get("data")
		items, _ := data.(wire.Array)
		more := hasMore(page, d.m.v2)
		label := fmt.Sprintf("%s=%s", l.Field, value)
		switch {
		case len(items) == 0:
			resp.Diagnostics.AddError("Not found", fmt.Sprintf("no %s found for %s", d.m.decl.TypeName, label))
		case len(items) > 1 || more:
			count := strconv.Itoa(len(items))
			if more {
				count = "more than " + count
			}
			resp.Diagnostics.AddError("Ambiguous lookup", fmt.Sprintf("%s matches %s objects for %s; use `id`", d.m.decl.TypeName, count, label))
		default:
			if obj, ok := items[0].(wire.Object); ok {
				return obj, nil
			}
			resp.Diagnostics.AddError("Unexpected Stripe response", "list item is not an object")
		}
		return nil, nil
	}
	resp.Diagnostics.AddError("Invalid lookup", "no lookup value is set")
	return nil, nil
}
