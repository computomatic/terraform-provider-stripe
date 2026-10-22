package acctest

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// Removed renders a `removed` block with `lifecycle { destroy = false }` for each address. A
// test of a resource whose destroy is refused ends with a step that replaces the resource's
// block with these, because the harness's closing destroy would fail on the refusal.
func Removed(addresses ...string) string {
	var b strings.Builder
	for _, a := range addresses {
		fmt.Fprintf(&b, "removed {\n  from = %s\n\n  lifecycle {\n    destroy = false\n  }\n}\n", a)
	}
	return b.String()
}

// Forget appends to steps the step that ends a test of a resource whose destroy is refused. It
// forgets every resource of the given types, or at the given addresses such as
// `stripe_billing_portal_configuration.default`, that any step configures: the last step's
// configuration without their blocks and every block that refers to one of them, with Removed
// for each. check, when not nil, runs after that step, such as to read the object through the
// API and find it in place. A block that refers to a forgotten one, such as a data source that
// reads it, is dropped from the configuration and so destroyed as usual.
func Forget(t *testing.T, steps []resource.TestStep, check resource.TestCheckFunc, types ...string) []resource.TestStep {
	t.Helper()
	var addresses []string
	config := ""
	for _, step := range steps {
		if step.Config == "" {
			continue
		}
		config = step.Config
		found, err := resourcesOf(step.Config, types)
		if err != nil {
			t.Fatalf("forgetting %v: %v", types, err)
		}
		for _, a := range found {
			if !slices.Contains(addresses, a) {
				addresses = append(addresses, a)
			}
		}
	}
	if len(addresses) == 0 {
		t.Fatalf("forgetting %v: no step configures one", types)
	}
	kept, err := without(config, addresses)
	if err != nil {
		t.Fatalf("forgetting %v: %v", types, err)
	}
	return append(steps, resource.TestStep{Config: kept + Removed(addresses...), Check: check})
}

// Without is config without the resource blocks of the given types or addresses and every block that
// refers to one of them, such as a data source that reads it: the configuration of a step
// that drops the resource, which a `_delete` test expects to fail at plan time.
func Without(t *testing.T, config string, types ...string) string {
	t.Helper()
	addresses, err := resourcesOf(config, types)
	if err == nil {
		config, err = without(config, addresses)
	}
	if err != nil {
		t.Fatalf("dropping %v: %v", types, err)
	}
	return config
}

// resourcesOf lists the addresses of the resource blocks in config of the given types or at the
// given addresses.
func resourcesOf(config string, types []string) ([]string, error) {
	file, diags := hclwrite.ParseConfig([]byte(config), "config.tf", hcl.InitialPos)
	if diags.HasErrors() {
		return nil, diags
	}
	var out []string
	for _, block := range file.Body().Blocks() {
		if labels := block.Labels(); block.Type() == "resource" && len(labels) == 2 && (slices.Contains(types, labels[0]) || slices.Contains(types, labels[0]+"."+labels[1])) {
			out = append(out, labels[0]+"."+labels[1])
		}
	}
	return out, nil
}

// without drops from config the resource blocks at addresses and, until none is left, every
// block whose body refers to a dropped block.
func without(config string, addresses []string) (string, error) {
	file, diags := hclwrite.ParseConfig([]byte(config), "config.tf", hcl.InitialPos)
	if diags.HasErrors() {
		return "", diags
	}
	body := file.Body()
	dropped := map[string]bool{}
	for _, a := range addresses {
		dropped[a] = true
	}
	for changed := true; changed; {
		changed = false
		for _, block := range body.Blocks() {
			address := blockAddress(block)
			if dropped[address] && address != "" || refersTo(block, dropped) {
				body.RemoveBlock(block)
				if address != "" {
					dropped[address] = true
				}
				changed = true
			}
		}
	}
	return string(file.Bytes()), nil
}

// blockAddress is the address other blocks use to refer to a resource, data or output block,
// or "" for any other block.
func blockAddress(block *hclwrite.Block) string {
	labels := block.Labels()
	switch {
	case block.Type() == "resource" && len(labels) == 2:
		return labels[0] + "." + labels[1]
	case block.Type() == "data" && len(labels) == 2:
		return "data." + labels[0] + "." + labels[1]
	case block.Type() == "output" && len(labels) == 1:
		return "output." + labels[0]
	}
	return ""
}

// refersTo reports whether the block's body names one of the addresses as a traversal.
func refersTo(block *hclwrite.Block, addresses map[string]bool) bool {
	text := string(block.Body().BuildTokens(nil).Bytes())
	for a := range addresses {
		if regexp.MustCompile(`(^|[^.\w])` + regexp.QuoteMeta(a) + `\b`).MatchString(text) {
			return true
		}
	}
	return false
}
