package main

import (
	"encoding/json"
	"fmt"
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

// requestSchema returns the resolved request schema of an operation, preferring the form
// encoding (v1) and falling back to JSON (v2).
func (s *Spec) requestSchema(op *Operation) (*Schema, error) {
	if op.RequestBody == nil {
		return nil, nil
	}
	for _, mt := range []string{"application/x-www-form-urlencoded", "application/json"} {
		if m := op.RequestBody.Content[mt]; m != nil {
			return s.resolve(m.Schema)
		}
	}
	return nil, fmt.Errorf("%s has no form or JSON request body", op.OperationID)
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
