package engine

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// fixtureTable has one field for every row of the ADR D2 nested-object table, plus the
// request/response combinations the rules distinguish.
func fixtureTable() []fields.Field {
	return []fields.Field{
		{Name: "id", Shape: fields.String, Create: true, Response: true, ServerDefaulted: true},
		{Name: "created", Shape: fields.Int, Response: true},
		{Name: "name", Shape: fields.String, Required: true, Create: true, Update: true, Response: true, Description: "The display name."},
		{Name: "kind", Shape: fields.String, Create: true, Response: true, Enum: []string{"a", "b"}},
		{Name: "active", Shape: fields.Bool, Create: true, Update: true, Response: true, ServerDefaulted: true},
		{Name: "quantity", Shape: fields.Int, Create: true, Update: true, Response: true},
		{Name: "ratio", Shape: fields.Float, Create: true, Update: true, Response: true},
		{Name: "address", Shape: fields.Object, Create: true, Update: true, Response: true, Fields: []fields.Field{
			{Name: "line1", Shape: fields.String, Required: true, Create: true, Update: true, Response: true},
			{Name: "zip", Shape: fields.String, Create: true, Response: true},
		}},
		{Name: "items", Shape: fields.ObjectList, Create: true, Update: true, Response: true, Fields: []fields.Field{
			{Name: "sku", Shape: fields.String, Required: true, Create: true, Update: true, Response: true},
			{Name: "qty", Shape: fields.Int, Create: true, Update: true, Response: true},
		}},
		{Name: "events", Shape: fields.ScalarList, Elem: fields.String, Create: true, Update: true, Response: true},
		{Name: "tags", Shape: fields.ScalarList, Elem: fields.String, Create: true, Update: true, Response: true},
		{Name: "options", Shape: fields.ObjectMap, Create: true, Update: true, Response: true, Fields: []fields.Field{
			{Name: "amount", Shape: fields.Int, Create: true, Update: true, Response: true},
		}},
		{Name: "metadata", Shape: fields.StringMap, Create: true, Update: true, Response: true, ServerDefaulted: true},
		{Name: "secret", Shape: fields.String, Response: true},
		{Name: "default_ref", Shape: fields.String, Update: true, Response: true},
		{Name: "url", Shape: fields.String, Create: true, Update: true, Response: true},
		{Name: "passcode", Shape: fields.String, Create: true, Update: true},
		{Name: "provider", Shape: fields.String, Response: true},
	}
}

func fixtureDeclaration() Declaration {
	return Declaration{
		TypeName:    "stripe_widget",
		IDPrefix:    "wid_",
		Description: "A widget.",
		Paths: Paths{
			Create:   "/v1/widgets",
			Retrieve: "/v1/widgets/{id}",
			Update:   "/v1/widgets/{id}",
			Delete:   "/v1/widgets/{id}",
			List:     "/v1/widgets",
		},
		Delete:         Delete,
		OnceReturned:   []string{"secret"},
		Sensitive:      []string{"passcode"},
		PreserveConfig: []string{"url"},
		Sets:           []string{"events"},
		Rename:         map[string]string{"provider": "provider_"},
		Lookups:        []Lookup{{Field: "kind"}},
	}
}

func fixtureResource(t *testing.T, client Client) *Resource {
	t.Helper()
	return configured(t, fixtureDeclaration(), fixtureTable(), client)
}

func configured(t *testing.T, decl Declaration, table []fields.Field, client Client) *Resource {
	t.Helper()
	ctor, err := NewResource(decl, table)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := ctor().(*Resource)
	if !ok {
		t.Fatalf("constructor returned %T", ctor())
	}
	configureResource(t, r, client)
	return r
}

// fill builds a top-level object value: given attributes as set, computed attributes
// without a value unknown (as a plan has them), the rest null.
func (r *Resource) fill(set map[string]attr.Value) basetypes.ObjectValue {
	attrs := make(map[string]attr.Value, len(r.objType.AttrTypes))
	for name, typ := range r.objType.AttrTypes {
		switch v, given := set[name]; {
		case given:
			attrs[name] = v
		case r.schema.Attributes[name].IsComputed():
			attrs[name] = unknownOf(typ)
		default:
			attrs[name] = nullOf(typ)
		}
	}
	return types.ObjectValueMust(r.objType.AttrTypes, attrs)
}

func unknownOf(t attr.Type) attr.Value {
	switch t := t.(type) {
	case basetypes.ObjectType:
		return types.ObjectUnknown(t.AttrTypes)
	case basetypes.ListType:
		return types.ListUnknown(t.ElemType)
	case basetypes.SetType:
		return types.SetUnknown(t.ElemType)
	case basetypes.MapType:
		return types.MapUnknown(t.ElemType)
	case basetypes.BoolType:
		return types.BoolUnknown()
	case basetypes.Int64Type:
		return types.Int64Unknown()
	case basetypes.Float64Type:
		return types.Float64Unknown()
	}
	return types.StringUnknown()
}

// object builds a nested object value, with every attribute the type has and no value
// for the ones not given.
func object(t attr.Type, set map[string]attr.Value) basetypes.ObjectValue {
	ot := t.(basetypes.ObjectType)
	attrs := make(map[string]attr.Value, len(ot.AttrTypes))
	for name, typ := range ot.AttrTypes {
		if v, ok := set[name]; ok {
			attrs[name] = v
		} else {
			attrs[name] = nullOf(typ)
		}
	}
	return types.ObjectValueMust(ot.AttrTypes, attrs)
}

func strs(vals ...string) []attr.Value {
	out := make([]attr.Value, len(vals))
	for i, v := range vals {
		out[i] = types.StringValue(v)
	}
	return out
}

func stringMapValue(kv map[string]string) basetypes.MapValue {
	elems := map[string]attr.Value{}
	for k, v := range kv {
		elems[k] = types.StringValue(v)
	}
	return types.MapValueMust(types.StringType, elems)
}

// stubStripe is a tiny in-memory Stripe: it creates, retrieves, updates, deletes and
// lists one kind of object under /v1/widgets, and records every request.
type stubStripe struct {
	// prefix is the collection path, `/v1/widgets` or, for a /v2 stub, `/v2/core/widgets`.
	prefix string
	// morePages makes a /v2 list report a next page.
	morePages bool
	// omitEmpty leaves the empty collections out of a created object, as /v2 may.
	omitEmpty bool
	mu        sync.Mutex
	next      int
	objects   map[string]wire.Object
	log       []stripeapi.Request
}

func newStub() *stubStripe {
	return &stubStripe{prefix: "/v1/widgets", objects: map[string]wire.Object{}}
}

// newStubV2 serves the same widgets under /v2/core/widgets, with the /v2 list envelope.
func newStubV2() *stubStripe {
	return &stubStripe{prefix: "/v2/core/widgets", objects: map[string]wire.Object{}}
}

func (s *stubStripe) requests() []stripeapi.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]stripeapi.Request(nil), s.log...)
}

func (s *stubStripe) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log = nil
}

func notFound() error {
	return &stripeapi.APIError{Status: http.StatusNotFound, Code: "resource_missing", Message: "No such widget"}
}

func put(o wire.Object, name string, v wire.Value) wire.Object {
	for i := range o {
		if o[i].Name == name {
			o[i].Value = v
			return o
		}
	}
	return append(o, wire.Field{Name: name, Value: v})
}

func drop(o wire.Object, name string) wire.Object {
	var out wire.Object
	for _, f := range o {
		if f.Name != name {
			out = append(out, f)
		}
	}
	return out
}

func (s *stubStripe) Do(_ context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log = append(s.log, req)
	ok := func(o wire.Object) (*stripeapi.Response, error) {
		return &stripeapi.Response{Status: 200, Body: o}, nil
	}

	prefix := s.prefix
	id := ""
	if len(req.Path) > len(prefix)+1 {
		id = req.Path[len(prefix)+1:]
	}
	switch {
	case req.Method == "POST" && id == "":
		s.next++
		obj := wire.Object{{Name: "object", Value: wire.String("widget")}}
		newID := "wid_" + strconv.Itoa(s.next)
		if custom, has := req.Params.GetString("id"); has {
			newID = custom
		}
		obj = put(obj, "id", wire.String(newID))
		obj = put(obj, "created", wire.Int(100))
		obj = put(obj, "active", wire.Bool(true))
		if !s.omitEmpty {
			obj = put(obj, "metadata", wire.Object{})
			obj = put(obj, "tags", wire.Array{})
			obj = put(obj, "events", wire.Array{})
		}
		obj = put(obj, "secret", wire.String("whsec_"+newID))
		for _, f := range req.Params {
			obj = s.apply(obj, f)
		}
		s.objects[newID] = obj
		return ok(obj)
	case req.Method == "GET" && id == "":
		var data wire.Array
		for _, o := range s.objects {
			if kind, has := req.Params.GetString("kind"); has {
				if got, _ := o.GetString("kind"); got != kind {
					continue
				}
			}
			data = append(data, o)
		}
		if strings.HasPrefix(prefix, "/v2/") {
			var next wire.Value = wire.Null{}
			if s.morePages {
				next = wire.String("/v2/core/widgets?page=next")
			}
			return ok(wire.Object{{Name: "data", Value: data}, {Name: "next_page_url", Value: next}, {Name: "previous_page_url", Value: wire.Null{}}})
		}
		return ok(wire.Object{{Name: "data", Value: data}, {Name: "has_more", Value: wire.Bool(false)}})
	}

	obj, exists := s.objects[id]
	if !exists {
		return nil, notFound()
	}
	switch req.Method {
	case "GET":
		return ok(obj)
	case "POST":
		for _, f := range req.Params {
			obj = s.apply(obj, f)
		}
		s.objects[id] = obj
		return ok(obj)
	default:
		delete(s.objects, id)
		return ok(wire.Object{{Name: "deleted", Value: wire.Bool(true)}})
	}
}

// apply sets one request parameter the way Stripe would: null unsets, metadata merges key
// by key and `url` reads back redacted.
func (s *stubStripe) apply(obj wire.Object, f wire.Field) wire.Object {
	switch v := f.Value.(type) {
	case wire.Null:
		return drop(obj, f.Name)
	case wire.Object:
		if f.Name == "metadata" {
			meta, _ := obj.GetObject("metadata")
			for _, kv := range v {
				if _, unset := kv.Value.(wire.Null); unset {
					meta = drop(meta, kv.Name)
				} else {
					meta = put(meta, kv.Name, kv.Value)
				}
			}
			return put(obj, "metadata", meta)
		}
	}
	if f.Name == "url" {
		return put(obj, "url", wire.String("https://redacted.example"))
	}
	if f.Name == "passcode" {
		return obj
	}
	return put(obj, f.Name, f.Value)
}
