package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// Schemas is the part of `terraform providers schema -json` the diff reads.
type Schemas struct {
	Providers map[string]ProviderSchema `json:"provider_schemas"`
}

// ProviderSchema holds the resource and data source schemas of one provider.
type ProviderSchema struct {
	Resources   map[string]Schema `json:"resource_schemas"`
	DataSources map[string]Schema `json:"data_source_schemas"`
}

// Schema is the schema of one resource or data source.
type Schema struct {
	Block Block `json:"block"`
}

// Block is a set of attributes and nested blocks.
type Block struct {
	Attributes map[string]Attribute `json:"attributes"`
	BlockTypes map[string]BlockType `json:"block_types"`
}

// BlockType is a nested block.
type BlockType struct {
	NestingMode string `json:"nesting_mode"`
	Block       Block  `json:"block"`
}

// Attribute is one attribute. Type is kept as the JSON Terraform printed, which is a
// string for primitives and an array for collections and objects.
type Attribute struct {
	Type       json.RawMessage `json:"type"`
	NestedType *NestedType     `json:"nested_type"`
	Required   bool            `json:"required"`
	Optional   bool            `json:"optional"`
	Computed   bool            `json:"computed"`
	Sensitive  bool            `json:"sensitive"`
}

// NestedType is a nested attribute's own attributes.
type NestedType struct {
	NestingMode string               `json:"nesting_mode"`
	Attributes  map[string]Attribute `json:"attributes"`
}

// Change is one difference between two schemas.
type Change struct {
	// Path names the resource or data source and the attribute, such as
	// `resource stripe_product.address.zip`.
	Path   string
	Kind   string
	Detail string
	// Breaking is true when a configuration or state that was valid before may not be
	// valid now.
	Breaking bool
}

func (c Change) String() string {
	mark := "      "
	if c.Breaking {
		mark = "BREAK "
	}
	s := fmt.Sprintf("%s%-8s %s", mark, c.Kind, c.Path)
	if c.Detail != "" {
		s += ": " + c.Detail
	}
	return s
}

// ParseSchemas decodes the JSON of `terraform providers schema -json`.
func ParseSchemas(data []byte) (*Schemas, error) {
	var s Schemas
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("decode provider schema: %w", err)
	}
	return &s, nil
}

// Diff lists what changed from old to updated, sorted by path. Breaking changes are a
// removed resource, data source or attribute (a rename is a removal and an addition), a
// type or nesting change, an optional attribute that became required, a settable
// attribute that became output-only, and an added required attribute.
func Diff(old, updated *Schemas) []Change {
	var changes []Change
	for name, o := range old.Providers {
		n, ok := updated.Providers[name]
		if !ok {
			changes = append(changes, Change{Path: "provider " + name, Kind: "removed", Breaking: true})
			continue
		}
		changes = append(changes, diffSchemas("resource", o.Resources, n.Resources)...)
		changes = append(changes, diffSchemas("data source", o.DataSources, n.DataSources)...)
	}
	sort.SliceStable(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes
}

// HasBreaking reports whether any change is breaking.
func HasBreaking(changes []Change) bool {
	for _, c := range changes {
		if c.Breaking {
			return true
		}
	}
	return false
}

func diffSchemas(kind string, old, updated map[string]Schema) []Change {
	var changes []Change
	for name, o := range old {
		n, ok := updated[name]
		if !ok {
			changes = append(changes, Change{Path: kind + " " + name, Kind: "removed", Breaking: true})
			continue
		}
		changes = diffBlock(changes, kind+" "+name, o.Block, n.Block)
	}
	for name := range updated {
		if _, ok := old[name]; !ok {
			changes = append(changes, Change{Path: kind + " " + name, Kind: "added"})
		}
	}
	return changes
}

func diffBlock(changes []Change, prefix string, old, updated Block) []Change {
	changes = diffAttributes(changes, prefix, old.Attributes, updated.Attributes)
	for name, o := range old.BlockTypes {
		path := prefix + "." + name
		n, ok := updated.BlockTypes[name]
		switch {
		case !ok:
			changes = append(changes, Change{Path: path, Kind: "removed", Detail: "block", Breaking: true})
		case o.NestingMode != n.NestingMode:
			changes = append(changes, Change{Path: path, Kind: "changed", Detail: fmt.Sprintf("block nesting %s to %s", o.NestingMode, n.NestingMode), Breaking: true})
		default:
			changes = diffBlock(changes, path, o.Block, n.Block)
		}
	}
	for name := range updated.BlockTypes {
		if _, ok := old.BlockTypes[name]; !ok {
			changes = append(changes, Change{Path: prefix + "." + name, Kind: "added", Detail: "block"})
		}
	}
	return changes
}

func diffAttributes(changes []Change, prefix string, old, updated map[string]Attribute) []Change {
	for name, o := range old {
		path := prefix + "." + name
		n, ok := updated[name]
		if !ok {
			changes = append(changes, Change{Path: path, Kind: "removed", Breaking: true})
			continue
		}
		changes = append(changes, diffAttribute(path, o, n)...)
		if o.NestedType != nil && n.NestedType != nil {
			changes = diffAttributes(changes, path, o.NestedType.Attributes, n.NestedType.Attributes)
		}
	}
	for name, n := range updated {
		if _, ok := old[name]; !ok {
			detail := ""
			if n.Required {
				detail = "required"
			}
			changes = append(changes, Change{Path: prefix + "." + name, Kind: "added", Detail: detail, Breaking: n.Required})
		}
	}
	return changes
}

func diffAttribute(path string, o, n Attribute) []Change {
	var changes []Change
	if !sameJSON(o.Type, n.Type) {
		changes = append(changes, Change{Path: path, Kind: "changed", Detail: fmt.Sprintf("type %s to %s", o.Type, n.Type), Breaking: true})
	}
	if (o.NestedType == nil) != (n.NestedType == nil) ||
		(o.NestedType != nil && o.NestedType.NestingMode != n.NestedType.NestingMode) {
		changes = append(changes, Change{Path: path, Kind: "changed", Detail: "nesting", Breaking: true})
	}
	if !o.Required && n.Required {
		changes = append(changes, Change{Path: path, Kind: "changed", Detail: "optional to required", Breaking: true})
	}
	if o.Required && !n.Required {
		changes = append(changes, Change{Path: path, Kind: "changed", Detail: "required to optional"})
	}
	if (o.Optional || o.Required) && !n.Optional && !n.Required {
		changes = append(changes, Change{Path: path, Kind: "changed", Detail: "settable to output-only", Breaking: true})
	}
	if o.Computed != n.Computed && !n.Required {
		changes = append(changes, Change{Path: path, Kind: "changed", Detail: fmt.Sprintf("computed %t to %t", o.Computed, n.Computed)})
	}
	if o.Sensitive != n.Sensitive {
		changes = append(changes, Change{Path: path, Kind: "changed", Detail: fmt.Sprintf("sensitive %t to %t", o.Sensitive, n.Sensitive)})
	}
	return changes
}

// sameJSON compares two JSON values by content, ignoring whitespace.
func sameJSON(a, b json.RawMessage) bool {
	var ca, cb bytes.Buffer
	if json.Compact(&ca, a) != nil || json.Compact(&cb, b) != nil {
		return bytes.Equal(a, b)
	}
	return bytes.Equal(ca.Bytes(), cb.Bytes())
}
