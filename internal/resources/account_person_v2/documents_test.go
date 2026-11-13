package account_person_v2_test

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

// uploads are the suite's inert test files for a person's documents (TestV2CoreAccountPersonAttributes):
// an `account_requirement` file and an `identity_document` file for each side of each
// verification, one set for the create and one for the update.
func uploads(t *testing.T) string {
	out := ""
	for i, f := range []struct{ name, purpose string }{
		{"requirement", "account_requirement"}, {"requirement_update", "account_requirement"},
		{"primary_front", "identity_document"}, {"primary_back", "identity_document"},
		{"primary_front_update", "identity_document"}, {"primary_back_update", "identity_document"},
		{"secondary_front", "identity_document"}, {"secondary_back", "identity_document"},
		{"secondary_front_update", "identity_document"}, {"secondary_back_update", "identity_document"},
	} {
		var b bytes.Buffer
		if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, i+1, i+1))); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), f.name+".png")
		if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		out += fmt.Sprintf("\nresource \"stripe_file\" %q {\n  source  = %q\n  purpose = %q\n}\n", f.name, path, f.purpose)
	}
	return out
}

// documentsConfig is a person on a merchant with every document (probe `update/6`), one with
// additional addresses (`update/5`) and one with an additional name (`update/8`), each holding
// the probe's create or, where updated, its update.
func documentsConfig(t *testing.T, c acctest.Config, updated bool) string {
	suffix, city, line1, postal, state, nameEntry := "", "Oakland", "500 Grand Ave", "94610", "CA", `{ full_name = "Jen Rosen", purpose = "alias" }`
	if updated {
		suffix, city, line1, postal, state, nameEntry = "_update", "Portland", "200 SW Market St", "97201", "OR", `{ purpose = "maiden" }`
	}
	return c.ProviderBlock() + accountBlock(c, "test") + uploads(t) + fmt.Sprintf(`
resource "stripe_account_person_v2" "documents" {
  account_id = stripe_account_v2.test.id
  documents = {
    company_authorization  = { files = [stripe_file.requirement%[1]s.id], type = "files" }
    passport               = { files = [stripe_file.requirement%[1]s.id], type = "files" }
    primary_verification   = { front_back = { front = stripe_file.primary_front%[1]s.id, back = stripe_file.primary_back%[1]s.id }, type = "front_back" }
    secondary_verification = { front_back = { front = stripe_file.secondary_front%[1]s.id, back = stripe_file.secondary_back%[1]s.id }, type = "front_back" }
    visa                   = { files = [stripe_file.requirement%[1]s.id], type = "files" }
  }
}

resource "stripe_account_person_v2" "addresses" {
  account_id = stripe_account_v2.test.id
  address    = { country = "US" }
  additional_addresses = [{
    city        = %[2]q
    country     = "US"
    line1       = %[3]q
    line2       = "Suite 5"
    postal_code = %[4]q
    purpose     = "registered"
    state       = %[5]q
    town        = "Downtown"
  }]
}

resource "stripe_account_person_v2" "names" {
  account_id       = stripe_account_v2.test.id
  additional_names = [%[6]s]
}
`, suffix, city, line1, postal, state, nameEntry)
}

// A person's documents, additional addresses and additional names change in place, each list
// and the documents sent whole, as the suite's probes send them. The maiden name replaces the
// alias, and the alias's full name goes with it.
func TestAccAccountPersonV2_documentsAndAdditions(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: documentsConfig(t, acct, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("stripe_account_person_v2.documents", "documents.passport.files.0", "stripe_file.requirement", "id"),
					resource.TestCheckResourceAttrPair("stripe_account_person_v2.documents", "documents.primary_verification.front_back.back", "stripe_file.primary_back", "id"),
					resource.TestCheckResourceAttr("stripe_account_person_v2.addresses", "additional_addresses.0.city", "Oakland"),
					resource.TestCheckResourceAttr("stripe_account_person_v2.names", "additional_names.0.full_name", "Jen Rosen"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config: documentsConfig(t, acct, true),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("stripe_account_person_v2.documents", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("stripe_account_person_v2.addresses", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("stripe_account_person_v2.names", plancheck.ResourceActionUpdate),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("stripe_account_person_v2.documents", "documents.visa.files.0", "stripe_file.requirement_update", "id"),
					resource.TestCheckResourceAttrPair("stripe_account_person_v2.documents", "documents.secondary_verification.front_back.front", "stripe_file.secondary_front_update", "id"),
					resource.TestCheckResourceAttr("stripe_account_person_v2.addresses", "additional_addresses.0.state", "OR"),
					resource.TestCheckResourceAttr("stripe_account_person_v2.names", "additional_names.0.purpose", "maiden"),
					resource.TestCheckNoResourceAttr("stripe_account_person_v2.names", "additional_names.0.full_name"),
				),
			},
		}, nil, "stripe_account_v2", "stripe_file"),
	})
}
