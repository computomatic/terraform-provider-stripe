package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Spec is the part of an OpenAPI 3 document the generator reads. Every other member is
// ignored by the decoder.
type Spec struct {
	Info struct {
		Version string `json:"version"`
	} `json:"info"`
	Paths      map[string]map[string]*Operation `json:"paths"`
	Components struct {
		Schemas map[string]*Schema `json:"schemas"`
	} `json:"components"`
}

// Operation is one method on a path.
type Operation struct {
	OperationID string              `json:"operationId"`
	RequestBody *Body               `json:"requestBody"`
	Responses   map[string]*Content `json:"responses"`
}

// Body is a request body.
type Body struct {
	Content map[string]*MediaType `json:"content"`
}

// Content is a response.
type Content struct {
	Content map[string]*MediaType `json:"content"`
}

// MediaType holds a body schema.
type MediaType struct {
	Schema *Schema `json:"schema"`
}

// Schema is an OpenAPI schema object, reduced to what the generator classifies.
type Schema struct {
	Ref                  string             `json:"$ref"`
	Type                 string             `json:"type"`
	Format               string             `json:"format"`
	Description          string             `json:"description"`
	Nullable             bool               `json:"nullable"`
	Required             []string           `json:"required"`
	Properties           map[string]*Schema `json:"properties"`
	AdditionalProperties *Additional        `json:"additionalProperties"`
	Items                *Schema            `json:"items"`
	AnyOf                []*Schema          `json:"anyOf"`
	OneOf                []*Schema          `json:"oneOf"`
	AllOf                []*Schema          `json:"allOf"`
	Enum                 []json.RawMessage  `json:"enum"`
	// ResourceID names the API object a component schema is (`x-resourceId`).
	ResourceID string `json:"x-resourceId"`
	// ExpansionResources is present on a field Stripe expands on request
	// (`x-expansionResources`); the leaf walk reads only whether it is there.
	ExpansionResources json.RawMessage `json:"x-expansionResources"`
}

// Additional is `additionalProperties`, which is either a boolean or a schema.
type Additional struct {
	Schema *Schema
}

// UnmarshalJSON accepts a boolean (kept as no schema) or a schema.
func (a *Additional) UnmarshalJSON(data []byte) error {
	var b bool
	if json.Unmarshal(data, &b) == nil {
		return nil
	}
	a.Schema = new(Schema)
	return json.Unmarshal(data, a.Schema)
}

// ParseSpec decodes an OpenAPI document.
func ParseSpec(data []byte) (*Spec, error) {
	var s Spec
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("decode OpenAPI document: %w", err)
	}
	return &s, nil
}

const refPrefix = "#/components/schemas/"

// resolve follows $ref links to the schema they name. The description of the first schema
// that has one wins, since Stripe documents a field on the reference, not on the target.
func (s *Spec) resolve(sc *Schema) (*Schema, error) {
	if sc == nil {
		return nil, nil
	}
	desc := sc.Description
	for hops := 0; sc.Ref != ""; hops++ {
		name, ok := strings.CutPrefix(sc.Ref, refPrefix)
		if !ok || hops > 16 {
			return nil, fmt.Errorf("unsupported reference %q", sc.Ref)
		}
		target := s.Components.Schemas[name]
		if target == nil {
			return nil, fmt.Errorf("reference %q names no schema", sc.Ref)
		}
		sc = target
		if desc == "" {
			desc = sc.Description
		}
	}
	if desc == sc.Description {
		return sc, nil
	}
	c := *sc
	c.Description = desc
	return &c, nil
}

// operation returns the operation for a method and path.
func (s *Spec) operation(method, path string) (*Operation, error) {
	op := s.Paths[path][strings.ToLower(method)]
	if op == nil {
		return nil, fmt.Errorf("spec has no %s %s", method, path)
	}
	return op, nil
}

// requestSchema returns the resolved request schema of the operation at path. A `/v2`
// operation must take JSON only and any other must take a form, so a spec release that
// changes a namespace's encoding fails generation instead of producing a wrong table. A
// multipart form, which the Files API's upload takes, is a form.
func (s *Spec) requestSchema(op *Operation, path string) (*Schema, error) {
	if op.RequestBody == nil {
		return nil, nil
	}
	form := op.RequestBody.Content["application/x-www-form-urlencoded"]
	if form == nil {
		form = op.RequestBody.Content["multipart/form-data"]
	}
	js := op.RequestBody.Content["application/json"]
	switch {
	case strings.HasPrefix(path, "/v2/") && (js == nil || form != nil):
		return nil, fmt.Errorf("%s: a /v2 operation must take a JSON body and no form", op.OperationID)
	case !strings.HasPrefix(path, "/v2/") && form == nil:
		return nil, fmt.Errorf("%s: a /v1 operation must take a form body", op.OperationID)
	case form != nil:
		return s.resolve(form.Schema)
	default:
		return s.resolve(js.Schema)
	}
}

// responseSchema returns the resolved schema of an operation's 200 response.
func (s *Spec) responseSchema(op *Operation) (*Schema, error) {
	r := op.Responses["200"]
	if r == nil {
		return nil, fmt.Errorf("%s has no 200 response", op.OperationID)
	}
	m := r.Content["application/json"]
	if m == nil {
		return nil, fmt.Errorf("%s has no JSON 200 response", op.OperationID)
	}
	return s.resolve(m.Schema)
}

// listElement returns the resolved schema of the elements of a list response's `data` array.
func (s *Spec) listElement(list *Schema) (*Schema, error) {
	data := prop(list, "data")
	if data == nil || data.Items == nil {
		return nil, fmt.Errorf("the response is not a list: it has no `data` array")
	}
	return s.resolve(data.Items)
}

// flattenHash returns the create request with the hash held by property name spread into
// its top level: the hash's fields and required list replace the property. The hash is the
// object variant of the property's anyOf.
func (s *Spec) flattenHash(create *Schema, name string) (*Schema, error) {
	if create == nil || create.Properties[name] == nil {
		return nil, fmt.Errorf("the create request has no property %q", name)
	}
	holder, err := s.resolve(create.Properties[name])
	if err != nil {
		return nil, err
	}
	var hash *Schema
	for _, v := range slices.Concat(holder.AnyOf, holder.OneOf) {
		sc, err := s.resolve(v)
		if err != nil {
			return nil, err
		}
		if len(sc.Properties) > 0 {
			hash = sc
			break
		}
	}
	if hash == nil {
		return nil, fmt.Errorf("property %q has no variant that is a hash", name)
	}
	flat := &Schema{Type: create.Type, Properties: map[string]*Schema{}, Required: slices.Concat(create.Required, hash.Required)}
	for k, v := range create.Properties {
		if k != name {
			flat.Properties[k] = v
		}
	}
	for k, v := range hash.Properties {
		if flat.Properties[k] != nil {
			return nil, fmt.Errorf("property %q of the hash %q is also a top-level property", k, name)
		}
		flat.Properties[k] = v
	}
	return flat, nil
}
