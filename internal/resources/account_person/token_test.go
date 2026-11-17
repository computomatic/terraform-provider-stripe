package account_person_test

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/resources/account_person"
)

// Stripe never returns a person token, and the record proves no unset of it, so the token is
// computed, as an account's is: removing it from the configuration sends nothing, rather than an
// empty `person_token` (engine: TestRemovingAComputedNeverReturnedArgumentSendsNothing).
func TestPersonTokenRemovalSendsNothing(t *testing.T) {
	ctor, err := account_person.NewResource()
	if err != nil {
		t.Fatal(err)
	}
	r, ok := ctor().(*engine.Resource)
	if !ok {
		t.Fatalf("constructor returned %T", ctor())
	}
	var resp resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if a := resp.Schema.Attributes["person_token"]; a == nil || !a.IsOptional() || !a.IsComputed() {
		t.Fatalf("person_token is not Optional and Computed: %v", a)
	}
	if got := r.AttributeDocs()["person_token"].Removal; got != engine.RemovalNotReturned {
		t.Errorf("person_token's removal sentence is %q, want %q", got, engine.RemovalNotReturned)
	}
}
