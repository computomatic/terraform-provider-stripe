package engine

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/product"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

func productDeclaration() Declaration {
	return Declaration{
		TypeName: "stripe_product",
		IDPrefix: "prod_",
		Paths: Paths{
			Create:   "/v1/products",
			Retrieve: "/v1/products/{id}",
			Update:   "/v1/products/{id}",
			Delete:   "/v1/products/{id}",
			List:     "/v1/products",
		},
		Delete: Delete,
	}
}

// The generated product table drives the whole lifecycle against the fake, each test on
// its own account.
func TestGeneratedProductAgainstFake(t *testing.T) {
	acct := acctest.NewAccount(t)
	client := stripeapi.New(stripeapi.Config{APIKey: acct.Key, BaseURL: acct.BaseURL, MaxRetries: 3}, stripeapi.Options{Version: "test"})
	r := configured(t, productDeclaration(), product.Table, client)

	dims := r.objType.AttrTypes["package_dimensions"]
	state, _ := r.create(t, r.fill(map[string]attr.Value{
		"name":        types.StringValue("Gold plan"),
		"description": types.StringValue("The gold one"),
		"metadata":    stringMapValue(map[string]string{"team": "a", "tier": "1"}),
		"package_dimensions": object(dims, map[string]attr.Value{
			"height": types.Float64Value(1.5), "length": types.Float64Value(2), "weight": types.Float64Value(3), "width": types.Float64Value(4),
		}),
	}))
	id := str(t, state, "id")
	if id == "" || str(t, state, "name") != "Gold plan" || !state.Attributes()["active"].Equal(types.BoolValue(true)) {
		t.Fatalf("created: %v", state)
	}
	if !state.Attributes()["url"].IsNull() || !state.Attributes()["tax_code"].IsNull() {
		t.Errorf("unset optional fields must stay null: %v", state)
	}

	read, found := r.read(t, state)
	if !found || !read.Equal(state) {
		t.Fatalf("a read straight after create must not drift:\n%v\n%v", read, state)
	}

	// Change the object behind the provider's back, as another user would.
	if _, err := client.Do(t.Context(), stripeapi.Request{Method: "POST", Path: "/v1/products/" + id, Params: wire.Object{{Name: "name", Value: wire.String("Renamed")}}}); err != nil {
		t.Fatal(err)
	}
	drifted, _ := r.read(t, state)
	if str(t, drifted, "name") != "Renamed" {
		t.Fatalf("drift not detected: %v", drifted)
	}

	planAttrs := map[string]attr.Value{}
	for k, v := range state.Attributes() {
		planAttrs[k] = v
	}
	planAttrs["name"] = types.StringValue("Gold plan v2")
	planAttrs["description"] = nullOf(types.StringType)
	planAttrs["metadata"] = stringMapValue(map[string]string{"team": "b"})
	planAttrs["updated"] = types.Int64Unknown()
	updated := r.update(t, r.fill(planAttrs), state)
	if str(t, updated, "name") != "Gold plan v2" || !updated.Attributes()["description"].IsNull() {
		t.Fatalf("updated: %v", updated)
	}
	if !updated.Attributes()["metadata"].Equal(stringMapValue(map[string]string{"team": "b"})) {
		t.Errorf("metadata after the diff update: %v", updated.Attributes()["metadata"])
	}

	// The provider sent each POST with an idempotency key and never sent `id` on update.
	log := acctest.RequestLog(t, acct)
	posts := 0
	for _, entry := range log {
		if entry.Method != "POST" {
			continue
		}
		posts++
		if entry.IdempotencyKey == "" {
			t.Errorf("POST %s without an idempotency key", entry.Path)
		}
		if params, ok := entry.Params.(wire.Object); ok {
			if _, sent := params.Get("id"); sent {
				t.Errorf("POST %s sent a create-only field: %v", entry.Path, params)
			}
		}
	}
	if posts != 3 {
		t.Errorf("%d POSTs, want create, out-of-band rename and update", posts)
	}

	noErrors(t, r.destroy(t, updated))
	if _, found := r.read(t, updated); found {
		t.Error("the product must be gone after destroy")
	}
}
