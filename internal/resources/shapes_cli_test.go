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

type shapeProvider struct{ store *shapeStore }

func (p *shapeProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "stripe"
}

func (p *shapeProvider) Schema(context.Context, provider.SchemaRequest, *provider.SchemaResponse) {}

func (p *shapeProvider) Configure(_ context.Context, _ provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	resp.ResourceData = &engine.ProviderData{Client: p.store}
}

func (p *shapeProvider) Resources(context.Context) []func() resource.Resource {
	var out []func() resource.Resource
	for typeName, base := range map[string]string{"stripe_shape": "/v1/shapes", "stripe_shape_v2": "/v2/core/shapes"} {
		ctor, err := engine.NewResource(engine.Declaration{
			TypeName:    typeName,
			IDPrefix:    "shp_",
			Description: "A fixture object with the shapes of D3.",
			Paths:       engine.Paths{Create: base, Retrieve: base + "/{id}", Update: base + "/{id}", Delete: base + "/{id}"},
			Delete:      engine.Delete,
		}, shapeTable())
		if err != nil {
			panic(err)
		}
		out = append(out, ctor)
	}
	return out
}

func (p *shapeProvider) DataSources(context.Context) []func() datasource.DataSource { return nil }

// shapeStore answers the fixture's create, retrieve, update and delete under one base path,
// returns a bank account ID for a `source` sent as a hash, and records the last create's
// parameters.
type shapeStore struct {
	base    string
	mu      sync.Mutex
	objects map[string]wire.Object
	next    int
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
		s.objects[id] = obj
	case s.objects[id] == nil:
		return nil, &stripeapi.APIError{Status: http.StatusNotFound, Code: "resource_missing", Message: "No such shape: " + id}
	case req.Method == "POST":
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
