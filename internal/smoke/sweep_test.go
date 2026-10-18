package smoke_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/smoke"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

func sweeper(t *testing.T, c acctest.Config) *smoke.Sweeper {
	return &smoke.Sweeper{Client: tfacc.Client(c), Log: t.Logf}
}

// TestSmokeSweep is what the smoke bundle's entrypoint runs before and after the wave 1
// tests. It only ever runs against the shared sandbox account in smoke mode or against a
// fake account; NewAccount offers nothing else. Against the fake the account is new and
// the sweep has nothing to do.
func TestSmokeSweep(t *testing.T) {
	acct := acctest.NewAccount(t)
	swept, err := sweeper(t, acct).Run(t.Context())
	if err != nil {
		t.Fatalf("the sweep failed:\n%v", err)
	}
	t.Logf("swept %d objects", len(swept))
}

func create(t *testing.T, acct acctest.Config, path string, params wire.Object) string {
	t.Helper()
	return tfacc.Create(t, acct, path, params)
}

func str(name, value string) wire.Field { return wire.Field{Name: name, Value: wire.String(value)} }

// seedObjects makes one object of each type under name, plus a priced product. The
// webhook endpoint has no name, so the name goes in its description.
func seedObjects(t *testing.T, acct acctest.Config, name, event string) (ids map[string]string, price string) {
	t.Helper()
	ids = map[string]string{
		"customer": create(t, acct, "/v1/customers", wire.Object{str("name", name)}),
		"coupon": create(t, acct, "/v1/coupons", wire.Object{str("name", name), str("duration", "once"),
			{Name: "percent_off", Value: wire.Int(10)}}),
		"webhook_endpoint": create(t, acct, "/v1/webhook_endpoints", wire.Object{str("description", name),
			str("url", "https://example.com/hook"), {Name: "enabled_events", Value: wire.Array{wire.String("charge.succeeded")}}}),
		"billing_meter": create(t, acct, "/v1/billing/meters", wire.Object{str("display_name", name), str("event_name", event),
			{Name: "default_aggregation", Value: wire.Object{str("formula", "count")}}}),
		"product":               create(t, acct, "/v1/products", wire.Object{str("name", name)}),
		"product_without_price": create(t, acct, "/v1/products", wire.Object{str("name", name+"-bare")}),
	}
	price = create(t, acct, "/v1/prices", wire.Object{str("currency", "usd"), {Name: "unit_amount", Value: wire.Int(500)},
		str("product", ids["product"])})
	return ids, price
}

func get(t *testing.T, acct acctest.Config, path string) (wire.Object, bool) {
	t.Helper()
	resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: path})
	if stripeapi.IsNotFound(err) {
		return nil, false
	}
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	obj := resp.Body.(wire.Object)
	if deleted, _ := obj.GetBool("deleted"); deleted {
		return nil, false
	}
	return obj, true
}

var paths = map[string]string{
	"customer": "/v1/customers/", "coupon": "/v1/coupons/", "webhook_endpoint": "/v1/webhook_endpoints/",
	"billing_meter": "/v1/billing/meters/", "product": "/v1/products/", "product_without_price": "/v1/products/",
}

func TestSweepAgainstFake(t *testing.T) {
	acctest.RequireFake(t)
	acct := acctest.NewAccount(t)
	ours, ourPrice := seedObjects(t, acct, smoke.Prefix+"run-1", "tfsmoke_event_a")
	otherIDs, otherPrice := seedObjects(t, acct, "keep-me", "keep_event")
	// Names that look like ours but do not start with the prefix.
	lookalikes, lookalikePrice := seedObjects(t, acct, "x-tfsmoke-run-2", "tfsmoke_event_b")
	upper, _ := seedObjects(t, acct, "TFSMOKE-run-3", "tfsmoke_event_c")

	// destroy archives a product and leaves a price made out of band active.
	offProduct := create(t, acct, "/v1/products", wire.Object{str("name", smoke.Prefix+"run-1-off")})
	offPrice := create(t, acct, "/v1/prices", wire.Object{str("currency", "usd"), {Name: "unit_amount", Value: wire.Int(700)}, str("product", offProduct)})
	tfacc.Post(t, acct, "/v1/products/"+offProduct, wire.Object{{Name: "active", Value: wire.Bool(false)}})

	swept, err := sweeper(t, acct).Run(t.Context())
	if err != nil {
		t.Fatalf("the sweep failed:\n%v", err)
	}

	for typ, id := range ours {
		obj, exists := get(t, acct, paths[typ]+id)
		switch typ {
		case "customer", "coupon", "webhook_endpoint", "product_without_price":
			if exists {
				t.Errorf("%s %s should be deleted", typ, id)
			}
		case "billing_meter":
			if status, _ := obj.GetString("status"); status != "inactive" {
				t.Errorf("meter %s status = %q, want inactive", id, status)
			}
		case "product":
			if active, _ := obj.GetBool("active"); active {
				t.Errorf("product %s should be archived", id)
			}
		}
	}
	if price, _ := get(t, acct, "/v1/prices/"+ourPrice); price == nil {
		t.Errorf("price %s should still exist, archived", ourPrice)
	} else if active, _ := price.GetBool("active"); active {
		t.Errorf("price %s should be archived", ourPrice)
	}

	for label, group := range map[string]map[string]string{"other": otherIDs, "lookalike": lookalikes, "uppercase": upper} {
		for typ, id := range group {
			obj, exists := get(t, acct, paths[typ]+id)
			if !exists {
				t.Errorf("%s %s %s was removed", label, typ, id)
				continue
			}
			if active, ok := obj.GetBool("active"); ok && !active {
				t.Errorf("%s %s %s was archived", label, typ, id)
			}
			if status, _ := obj.GetString("status"); status == "inactive" {
				t.Errorf("%s %s %s was deactivated", label, typ, id)
			}
		}
	}
	for _, id := range []string{otherPrice, lookalikePrice} {
		if price, _ := get(t, acct, "/v1/prices/"+id); price == nil || !isActive(price) {
			t.Errorf("price %s was touched", id)
		}
	}

	var ids []string
	for _, s := range swept {
		ids = append(ids, s.ID)
		if !strings.HasPrefix(s.Name, smoke.Prefix) && s.Type != "price" {
			t.Errorf("swept %s", s)
		}
	}
	for _, id := range ours {
		if !slices.Contains(ids, id) {
			t.Errorf("%s is missing from the swept list", id)
		}
	}
	if !slices.Contains(ids, ourPrice) {
		t.Errorf("price %s is missing from the swept list", ourPrice)
	}
	if price, _ := get(t, acct, "/v1/prices/"+offPrice); price == nil || isActive(price) {
		t.Errorf("price %s of the archived product should be archived", offPrice)
	}
	if !slices.Contains(ids, offPrice) || slices.Contains(ids, offProduct) {
		t.Errorf("swept %v: want the archived product's price and not the product", ids)
	}
	if len(swept) != len(ours)+2 {
		t.Errorf("swept %d objects, want %d", len(swept), len(ours)+2)
	}

	again, err := sweeper(t, acct).Run(t.Context())
	if err != nil {
		t.Fatalf("the second sweep failed:\n%v", err)
	}
	if len(again) != 0 {
		t.Errorf("a second sweep acted on %v", again)
	}
}

func isActive(obj wire.Object) bool {
	active, ok := obj.GetBool("active")
	return ok && active
}
