package main

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
)

// nesting is how a nested attribute holds its objects.
type nesting int

const (
	nestNone nesting = iota
	nestSingle
	nestList
	nestSet
	nestMap
)

// attr is one schema attribute, reduced to what a page shows.
type attr struct {
	Name        string
	Description string
	Required    bool
	Optional    bool
	Computed    bool
	Sensitive   bool
	WriteOnly   bool
	Deprecated  bool
	Nesting     nesting
	// Children are the fields of a nested attribute, sorted by name.
	Children []*attr
}

// argument reports whether the user sets the attribute.
func (a *attr) argument() bool { return a.Required || a.Optional }

// identityAttr is one attribute of a resource identity.
type identityAttr struct {
	Name        string
	Type        string
	Description string
	Required    bool
}

// schema is one resource, data source or the provider.
type schema struct {
	// Type is the full type name, such as `stripe_coupon`. It is empty for the provider.
	Type        string
	Description string
	// Attrs are the top-level attributes, sorted by name.
	Attrs []*attr
	// Identity is empty for a data source and the provider.
	Identity []*identityAttr
	// ImportNote is a resource's note for the Import section, from its declaration.
	ImportNote      string
	ImportNoteLevel engine.NoteLevel
}

func (s *schema) find(name string) *attr {
	for _, a := range s.Attrs {
		if a.Name == name {
			return a
		}
	}
	return nil
}

// schemas is everything a page set needs from the provider.
type schemas struct {
	Provider    *schema
	Resources   []*schema
	DataSources []*schema
}

// readSchemas asks a provider server for its schemas.
func readSchemas(ctx context.Context, server tfprotov6.ProviderServer) (*schemas, error) {
	resp, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		return nil, err
	}
	if err := diagnosticsError(resp.Diagnostics); err != nil {
		return nil, err
	}
	ids, err := server.GetResourceIdentitySchemas(ctx, &tfprotov6.GetResourceIdentitySchemasRequest{})
	if err != nil {
		return nil, err
	}
	if err := diagnosticsError(ids.Diagnostics); err != nil {
		return nil, err
	}
	return convert(resp, ids)
}

func diagnosticsError(diags []*tfprotov6.Diagnostic) error {
	for _, d := range diags {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			return fmt.Errorf("%s: %s", d.Summary, d.Detail)
		}
	}
	return nil
}

// convert turns the protocol schemas into the page model. It fails on anything it does not
// know how to document, so that a new schema feature cannot print a wrong page.
func convert(resp *tfprotov6.GetProviderSchemaResponse, ids *tfprotov6.GetResourceIdentitySchemasResponse) (*schemas, error) {
	out := &schemas{}
	var err error
	if out.Provider, err = convertSchema("", resp.Provider); err != nil {
		return nil, fmt.Errorf("provider: %w", err)
	}
	for _, name := range sortedKeys(resp.ResourceSchemas) {
		s, err := convertSchema(name, resp.ResourceSchemas[name])
		if err != nil {
			return nil, fmt.Errorf("resource %s: %w", name, err)
		}
		if id := ids.IdentitySchemas[name]; id != nil {
			if s.Identity, err = convertIdentity(id); err != nil {
				return nil, fmt.Errorf("resource %s: %w", name, err)
			}
		}
		out.Resources = append(out.Resources, s)
	}
	for _, name := range sortedKeys(resp.DataSourceSchemas) {
		s, err := convertSchema(name, resp.DataSourceSchemas[name])
		if err != nil {
			return nil, fmt.Errorf("data source %s: %w", name, err)
		}
		out.DataSources = append(out.DataSources, s)
	}
	return out, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func convertSchema(typeName string, s *tfprotov6.Schema) (*schema, error) {
	if s == nil || s.Block == nil {
		return nil, fmt.Errorf("no schema")
	}
	if len(s.Block.BlockTypes) > 0 {
		return nil, fmt.Errorf("nested blocks are not supported")
	}
	attrs, err := convertAttrs(s.Block.Attributes)
	if err != nil {
		return nil, err
	}
	return &schema{Type: typeName, Description: s.Block.Description, Attrs: attrs}, nil
}

func convertAttrs(in []*tfprotov6.SchemaAttribute) ([]*attr, error) {
	out := make([]*attr, 0, len(in))
	for _, p := range in {
		a := &attr{
			Name:        p.Name,
			Description: p.Description,
			Required:    p.Required,
			Optional:    p.Optional,
			Computed:    p.Computed,
			Sensitive:   p.Sensitive,
			WriteOnly:   p.WriteOnly,
			Deprecated:  p.Deprecated,
		}
		if p.NestedType != nil {
			switch p.NestedType.Nesting {
			case tfprotov6.SchemaObjectNestingModeSingle:
				a.Nesting = nestSingle
			case tfprotov6.SchemaObjectNestingModeList:
				a.Nesting = nestList
			case tfprotov6.SchemaObjectNestingModeSet:
				a.Nesting = nestSet
			case tfprotov6.SchemaObjectNestingModeMap:
				a.Nesting = nestMap
			default:
				return nil, fmt.Errorf("attribute %s: unknown nesting mode %v", p.Name, p.NestedType.Nesting)
			}
			children, err := convertAttrs(p.NestedType.Attributes)
			if err != nil {
				return nil, fmt.Errorf("%s.%w", p.Name, err)
			}
			a.Children = children
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func convertIdentity(id *tfprotov6.ResourceIdentitySchema) ([]*identityAttr, error) {
	out := make([]*identityAttr, 0, len(id.IdentityAttributes))
	for _, p := range id.IdentityAttributes {
		var typ string
		switch {
		case p.Type.Is(tftypes.String):
			typ = "String"
		case p.Type.Is(tftypes.Number):
			typ = "Number"
		case p.Type.Is(tftypes.Bool):
			typ = "Boolean"
		default:
			return nil, fmt.Errorf("identity attribute %s: unsupported type %s", p.Name, p.Type)
		}
		out = append(out, &identityAttr{Name: p.Name, Type: typ, Description: p.Description, Required: p.RequiredForImport})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
