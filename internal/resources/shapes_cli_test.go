package resources

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	tfresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// The shapes of ADR 2026-10-29-full-attribute-coverage, D3, under Terraform and OpenTofu: CI's
// acceptance job runs these under both. They need no fake: an in-memory object store stands
// in for Stripe.

// shapeTable has a hash with no members, `options.affirm`, and both forms of a token-or-hash
// parameter, `source` and `source_data`, whose response returns the token.
func shapeTable() []fields.Field {
	return []fields.Field{
		{Name: "id", Shape: fields.String, Response: true},
		{Name: "name", Shape: fields.String, Required: true, Create: true, Update: true, Response: true},
		{Name: "options", Shape: fields.Object, Create: true, Update: true, Fields: []fields.Field{
			{Name: "affirm", Shape: fields.Object, Create: true, Update: true},
		}},
		{Name: "source", Shape: fields.String, Create: true, Response: true},
		{Name: "source", Attr: "source_data", Shape: fields.Object, Create: true, Fields: []fields.Field{
			{Name: "number", Shape: fields.String, Create: true},
		}},
	}
}

// listTable has a list whose elements take a price as a token or as a hash; the response
// returns each element's token.
func listTable() []fields.Field {
	return []fields.Field{
		{Name: "id", Shape: fields.String, Response: true},
		{Name: "items", Shape: fields.ObjectList, Required: true, Create: true, Update: true, Response: true, Fields: []fields.Field{
			{Name: "price", Shape: fields.String, Create: true, Update: true, Response: true},
			{Name: "price", Attr: "price_data", Shape: fields.Object, Create: true, Update: true, Fields: []fields.Field{
				{Name: "unit_amount", Shape: fields.Int, Create: true, Update: true},
			}},
			{Name: "quantity", Shape: fields.Int, Create: true, Update: true, Response: true},
		}},
	}
}

// fixedListTable is listTable with the list create-only, which forces replacement, and
// metadata, which changes in place.
func fixedListTable() []fields.Field {
	table := listTable()
	table[1].Update = false
	return append(table, fields.Field{Name: "metadata", Shape: fields.StringMap, Create: true, Update: true, Response: true})
}

func shapeFactories(store *shapeStore) map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){"stripe": providerserver.NewProtocol6WithError(&shapeProvider{store: store})}
}

// A request hash with no members, such as a payment method type under
// `payment_method_options`, is a nested attribute with no members, configured as `{}`: the
// framework and both CLIs accept it, and `/v2` sends it as `{}`.
func TestAccEmptyHashIsAcceptedByTheCLI(t *testing.T) {
	store := newShapeStore("/v2/core/shapes")
	tfresource.Test(t, tfresource.TestCase{
		ProtoV6ProviderFactories: shapeFactories(store),
		Steps: []tfresource.TestStep{
			{
				Config: `resource "stripe_shape_v2" "s" {
  name    = "one"
  options = { affirm = {} }
}`,
				Check: func(*terraform.State) error {
					opts, ok := store.created.GetObject("options")
					if affirm, has := opts.Get("affirm"); !ok || !has || len(affirm.(wire.Object)) != 0 {
						return fmt.Errorf("create sent %#v, want options.affirm as a hash with no members", store.created)
					}
					return nil
				},
			},
			{
				Config: `resource "stripe_shape_v2" "s" {
  name    = "two"
  options = { affirm = {} }
}`,
				Check: tfresource.TestCheckResourceAttr("stripe_shape_v2.s", "name", "two"),
			},
		},
	})
}

// On /v1 an empty value unsets a field, so a hash with no members is refused at plan until the
// conformance suite proves what Stripe takes.
func TestAccEmptyHashIsRefusedOnV1(t *testing.T) {
	tfresource.Test(t, tfresource.TestCase{
		ProtoV6ProviderFactories: shapeFactories(newShapeStore("/v1/shapes")),
		Steps: []tfresource.TestStep{{
			Config: `resource "stripe_shape" "s" {
  name    = "one"
  options = { affirm = {} }
}`,
			ExpectError: regexp.MustCompile("options.affirm"),
		}},
	})
}

// A create through the hash form records the ID Stripe returns in the token, and the next plan
// is empty.
func TestAccHashFormCreatesAndPlansClean(t *testing.T) {
	store := newShapeStore("/v1/shapes")
	config := `resource "stripe_shape" "s" {
  name        = "one"
  source_data = { number = "000123456789" }
}`
	tfresource.Test(t, tfresource.TestCase{
		ProtoV6ProviderFactories: shapeFactories(store),
		Steps: []tfresource.TestStep{
			{
				Config: config,
				Check: func(s *terraform.State) error {
					attrs := s.RootModule().Resources["stripe_shape.s"].Primary.Attributes
					if !strings.HasPrefix(attrs["source"], "ba_") || attrs["source_data.number"] != "000123456789" {
						return fmt.Errorf("state: %v", attrs)
					}
					if src, ok := store.created.GetObject("source"); !ok || len(src) != 1 {
						return fmt.Errorf("create sent %#v, want the hash under `source`", store.created)
					}
					return nil
				},
			},
			{Config: config, PlanOnly: true},
			{
				Config: `resource "stripe_shape" "s" {
  name        = "two"
  source_data = { number = "000123456789" }
}`,
				Check: tfresource.TestCheckResourceAttr("stripe_shape.s", "name", "two"),
			},
		},
	})
}

// An insert at the front of a list and a reorder, whose elements make equal prices through the
// hash form, take each element's token from Stripe, never from another element in the state.
func TestAccHashFormInAListKeepsEachElementsToken(t *testing.T) {
	store := newShapeStore("/v1/shape_lists")
	items := func(quantities ...int) string {
		var b strings.Builder
		b.WriteString(`resource "stripe_shape_list" "s" {
  items = [`)
		for _, q := range quantities {
			fmt.Fprintf(&b, "\n    { price_data = { unit_amount = 100 }, quantity = %d },", q)
		}
		b.WriteString("\n  ]\n}\n")
		return b.String()
	}
	tokens := func(want int) tfresource.TestCheckFunc {
		return func(s *terraform.State) error {
			attrs := s.RootModule().Resources["stripe_shape_list.s"].Primary.Attributes
			seen := map[string]bool{}
			for i := 0; i < want; i++ {
				token := attrs[fmt.Sprintf("items.%d.price", i)]
				if !strings.HasPrefix(token, "pr_") || seen[token] {
					return fmt.Errorf("items.%d.price = %q: %v", i, token, attrs)
				}
				seen[token] = true
			}
			return nil
		}
	}
	tfresource.Test(t, tfresource.TestCase{
		ProtoV6ProviderFactories: shapeFactories(store),
		Steps: []tfresource.TestStep{
			{Config: items(1), Check: tokens(1)},
			{Config: items(2, 1), Check: tokens(2)},
			{Config: items(1, 2), Check: tokens(2)},
		},
	})
}

// Under a create-only list, a token made through the hash form keeps its state value, so a
// change elsewhere updates the object in place instead of replacing it.
func TestAccHashFormUnderACreateOnlyListUpdatesInPlace(t *testing.T) {
	config := func(note string) string {
		return `resource "stripe_shape_fixed_list" "s" {
  items    = [{ price_data = { unit_amount = 100 }, quantity = 1 }]
  metadata = { note = "` + note + `" }
}`
	}
	tfresource.Test(t, tfresource.TestCase{
		ProtoV6ProviderFactories: shapeFactories(newShapeStore("/v1/shape_fixed_lists")),
		Steps: []tfresource.TestStep{
			{Config: config("one"), Check: tfresource.TestCheckResourceAttrSet("stripe_shape_fixed_list.s", "items.0.price")},
			{
				Config: config("two"),
				ConfigPlanChecks: tfresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("stripe_shape_fixed_list.s", plancheck.ResourceActionUpdate),
				}},
				Check: tfresource.TestCheckResourceAttr("stripe_shape_fixed_list.s", "metadata.note", "two"),
			},
		},
	})
}

type shapeProvider struct{ store *shapeStore }

func (p *shapeProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "stripe"
}

func (p *shapeProvider) Schema(context.Context, provider.SchemaRequest, *provider.SchemaResponse) {}

func (p *shapeProvider) Configure(_ context.Context, _ provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	resp.ResourceData = &engine.ProviderData{Client: p.store, Strict: true}
}

func (p *shapeProvider) Resources(context.Context) []func() resource.Resource {
	var out []func() resource.Resource
	for _, r := range []struct {
		typeName, base string
		table          []fields.Field
	}{{"stripe_shape", "/v1/shapes", shapeTable()}, {"stripe_shape_v2", "/v2/core/shapes", shapeTable()}, {"stripe_shape_list", "/v1/shape_lists", listTable()}, {"stripe_shape_fixed_list", "/v1/shape_fixed_lists", fixedListTable()}} {
		ctor, err := engine.NewResource(engine.Declaration{
			TypeName:    r.typeName,
			IDPrefix:    "shp_",
			Description: "A fixture object with the shapes of D3.",
			Paths:       engine.Paths{Create: r.base, Retrieve: r.base + "/{id}", Update: r.base + "/{id}", Delete: r.base + "/{id}"},
			Delete:      engine.Delete,
		}, r.table)
		if err != nil {
			panic(err)
		}
		out = append(out, ctor)
	}
	return out
}

func (p *shapeProvider) DataSources(context.Context) []func() datasource.DataSource { return nil }

// shapeStore answers the fixture's create, retrieve, update and delete under one base path,
// returns a bank account ID for a `source` sent as a hash and a new price ID for each element of
// `items` whose `price` is a hash, and records the last create's parameters.
type shapeStore struct {
	base    string
	mu      sync.Mutex
	objects map[string]wire.Object
	next    int
	prices  int
	created wire.Object
}

func newShapeStore(base string) *shapeStore {
	return &shapeStore{base: base, objects: map[string]wire.Object{}}
}

func (s *shapeStore) Do(_ context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rest, ok := strings.CutPrefix(req.Path, s.base)
	if !ok {
		return nil, &stripeapi.APIError{Status: http.StatusNotFound, Message: "Unrecognized request URL " + req.Path}
	}
	id := strings.TrimPrefix(rest, "/")
	switch {
	case req.Method == "POST" && id == "":
		s.next++
		id = "shp_" + strconv.Itoa(s.next)
		s.created = req.Params
		name, _ := req.Params.GetString("name")
		obj := wire.Object{{Name: "id", Value: wire.String(id)}, {Name: "object", Value: wire.String("shape")}, {Name: "name", Value: wire.String(name)}}
		if _, hash := req.Params.GetObject("source"); hash {
			obj = append(obj, wire.Field{Name: "source", Value: wire.String("ba_" + strconv.Itoa(s.next))})
		} else if token, has := req.Params.GetString("source"); has {
			obj = append(obj, wire.Field{Name: "source", Value: wire.String(token)})
		}
		obj, err := s.withItems(obj, req.Params)
		if err != nil {
			return nil, err
		}
		s.objects[id] = withMetadata(obj, req.Params)
	case s.objects[id] == nil:
		return nil, &stripeapi.APIError{Status: http.StatusNotFound, Code: "resource_missing", Message: "No such shape: " + id}
	case req.Method == "POST":
		obj, err := s.withItems(s.objects[id], req.Params)
		if err != nil {
			return nil, err
		}
		s.objects[id] = withMetadata(obj, req.Params)
		if name, ok := req.Params.GetString("name"); ok {
			obj := wire.Object{}
			for _, f := range s.objects[id] {
				if f.Name == "name" {
					f.Value = wire.String(name)
				}
				obj = append(obj, f)
			}
			s.objects[id] = obj
		}
	case req.Method == "DELETE":
		delete(s.objects, id)
		return &stripeapi.Response{Status: 200, Body: wire.Object{{Name: "id", Value: wire.String(id)}, {Name: "deleted", Value: wire.Bool(true)}}}, nil
	}
	return &stripeapi.Response{Status: 200, Body: s.objects[id]}, nil
}

// withItems sets the object's `items` from the request's, if it sends them: a price sent as a
// hash becomes a new price ID, one sent as an ID stays, and an element that sends a parameter
// twice is refused, as Stripe refuses one.
func (s *shapeStore) withItems(obj wire.Object, params wire.Object) (wire.Object, error) {
	sent, ok := params.Get("items")
	arr, isArr := sent.(wire.Array)
	if !ok || !isArr {
		return obj, nil
	}
	items := wire.Array{}
	for i, e := range arr {
		in, _ := e.(wire.Object)
		seen := map[string]bool{}
		for _, f := range in {
			if seen[f.Name] {
				return nil, &stripeapi.APIError{Status: http.StatusBadRequest, Type: "invalid_request_error", Param: fmt.Sprintf("items[%d][%s]", i, f.Name), Message: "Received both a string and a hash for " + f.Name}
			}
			seen[f.Name] = true
		}
		item := wire.Object{}
		if _, hash := in.GetObject("price"); hash {
			s.prices++
			item = append(item, wire.Field{Name: "price", Value: wire.String("pr_" + strconv.Itoa(s.prices))})
		} else if token, has := in.GetString("price"); has {
			item = append(item, wire.Field{Name: "price", Value: wire.String(token)})
		}
		if q, has := in.Get("quantity"); has {
			item = append(item, wire.Field{Name: "quantity", Value: q})
		}
		items = append(items, item)
	}
	out := wire.Object{}
	for _, f := range obj {
		if f.Name != "items" {
			out = append(out, f)
		}
	}
	return append(out, wire.Field{Name: "items", Value: items}), nil
}

// withMetadata sets the object's `metadata` from the request's keys, if it sends any; an empty
// value removes a key.
func withMetadata(obj wire.Object, params wire.Object) wire.Object {
	sent, ok := params.GetObject("metadata")
	if !ok {
		return obj
	}
	meta := wire.Object{}
	var out wire.Object
	for _, f := range obj {
		if f.Name == "metadata" {
			meta, _ = f.Value.(wire.Object)
			continue
		}
		out = append(out, f)
	}
	for _, kv := range sent {
		var kept wire.Object
		for _, f := range meta {
			if f.Name != kv.Name {
				kept = append(kept, f)
			}
		}
		if v, isStr := kv.Value.(wire.String); isStr && v != "" {
			kept = append(kept, kv)
		}
		meta = kept
	}
	return append(out, wire.Field{Name: "metadata", Value: meta})
}
