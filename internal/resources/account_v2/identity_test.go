package account_v2_test

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"maps"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

// change is one of the suite's update probes of a v2 account (TestV2CoreAccountAttributes): the
// identity the probe's account is created with beside the merchant configuration, the identity
// its update sends, and an attribute and the value each step reads back.
type change struct {
	entity, created, updated string
	attr, before, after      string
}

// identityChanges are the probes `update/<n>` that create a US merchant with one member of the
// individual's identity, the company's business details or its attestations, and update it.
var identityChanges = map[string]change{
	"dob":           {"individual", `individual = { date_of_birth = { day = 4, month = 3, year = 1980 } }`, `individual = { date_of_birth = { day = 5, month = 6, year = 1981 } }`, "identity.individual.date_of_birth.year", "1980", "1981"},                                                                                                                                                       // update/23
	"address":       {"individual", `individual = { address = { city = "South San Francisco", country = "US" } }`, `individual = { address = { city = "Seattle", country = "US" } }`, "identity.individual.address.city", "South San Francisco", "Seattle"},                                                                                                                                         // update/16
	"given_name":    {"individual", `individual = { given_name = "Jenny" }`, `individual = { given_name = "Jane" }`, "identity.individual.given_name", "Jenny", "Jane"},                                                                                                                                                                                                                             // update/32
	"surname":       {"individual", `individual = { surname = "Rosen" }`, `individual = { surname = "Roe" }`, "identity.individual.surname", "Rosen", "Roe"},                                                                                                                                                                                                                                        // update/44
	"legal_gender":  {"individual", `individual = { legal_gender = "female" }`, `individual = { legal_gender = "male" }`, "identity.individual.legal_gender", "female", "male"},                                                                                                                                                                                                                     // update/34
	"nationality":   {"individual", `individual = { nationalities = ["US"] }`, `individual = { nationalities = ["CA"] }`, "identity.individual.nationalities.0", "US", "CA"},                                                                                                                                                                                                                        // update/36
	"phone":         {"individual", `individual = { phone = "+18008675317" }`, `individual = { phone = "+18008675318" }`, "identity.individual.phone", "+18008675317", "+18008675318"},                                                                                                                                                                                                              // update/37
	"exposure":      {"individual", `individual = { political_exposure = "existing" }`, `individual = { political_exposure = "none" }`, "identity.individual.political_exposure", "existing", "none"},                                                                                                                                                                                               // update/38
	"ownership":     {"individual", `individual = { relationship = { owner = true, percent_ownership = "40.5" } }`, `individual = { relationship = { owner = true, percent_ownership = "25.5" } }`, "identity.individual.relationship.percent_ownership", "40.5", "25.5"},                                                                                                                           // update/42
	"title":         {"individual", `individual = { relationship = { owner = true, title = "CEO" } }`, `individual = { relationship = { owner = true, title = "CFO" } }`, "identity.individual.relationship.title", "CEO", "CFO"},                                                                                                                                                                   // update/43
	"other_address": {"individual", `individual = { additional_addresses = [{ city = "Oakland", country = "US", purpose = "registered" }] }`, `individual = { additional_addresses = [{ city = "Portland", country = "US", purpose = "registered" }] }`, "identity.individual.additional_addresses.0.city", "Oakland", "Portland"},                                                                  // update/7
	"maiden_name":   {"individual", `individual = { additional_names = [{ full_name = "Jen Rosen", purpose = "alias" }] }`, `individual = { additional_names = [{ purpose = "maiden" }] }`, "identity.individual.additional_names.0.purpose", "alias", "maiden"},                                                                                                                                    // update/14
	"company_city":  {"company", `business_details = { address = { city = "San Francisco", country = "US" } }`, `business_details = { address = { city = "Seattle", country = "US" } }`, "identity.business_details.address.city", "San Francisco", "Seattle"},                                                                                                                                      // update/61
	"company_state": {"company", `business_details = { address = { country = "US", state = "CA" } }`, `business_details = { address = { country = "US", state = "WA" } }`, "identity.business_details.address.state", "CA", "WA"},                                                                                                                                                                   // update/66
	"company_other": {"company", `business_details = { additional_addresses = [{ city = "Oakland", country = "US", purpose = "administrative" }] }`, `business_details = { additional_addresses = [{ city = "Portland", country = "US", purpose = "principal_place_of_business" }] }`, "identity.business_details.additional_addresses.0.purpose", "administrative", "principal_place_of_business"}, // update/54
	"revenue":       {"company", `business_details = { annual_revenue = { amount = { currency = "usd", value = 7 }, fiscal_year_end = "2025-12-31" } }`, `business_details = { annual_revenue = { amount = { currency = "usd", value = 8 }, fiscal_year_end = "2025-12-31" } }`, "identity.business_details.annual_revenue.amount.value", "7", "8"},                                                 // update/68
	"license":       {"company", `business_details = { documents = { company_license = { files = [stripe_file.requirement.id], type = "files" } } }`, `business_details = { documents = { company_license = { files = [stripe_file.requirement_update.id], type = "files" } } }`, "identity.business_details.documents.company_license.files.#", "1", "1"},                                          // update/71
}

// requirementFiles uploads the two inert `account_requirement` files the suite's document probes
// name, one for the create and one for the update.
func requirementFiles(t *testing.T) string {
	path := filepath.Join(t.TempDir(), "requirement.png")
	if err := os.WriteFile(path, squarePNG(t, 1), 0o600); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "requirement_update.png")
	if err := os.WriteFile(other, squarePNG(t, 2), 0o600); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf(`
resource "stripe_file" "requirement" {
  source  = %q
  purpose = "account_requirement"
}

resource "stripe_file" "requirement_update" {
  source  = %q
  purpose = "account_requirement"
}
`, path, other)
}

// squarePNG is a blank PNG of the given side, in pixels.
func squarePNG(t *testing.T, side int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, side, side))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// merchantWith is the suite's US merchant base: the card payments capability, no dashboard and
// the platform collecting fees and covering losses, with the given identity members.
func merchantWith(c acctest.Config, name, entity, identity, previous string) string {
	dependsOn := ""
	if previous != "" {
		dependsOn = fmt.Sprintf("  depends_on    = [stripe_account_v2.%s]\n", previous)
	}
	return fmt.Sprintf(`
resource "stripe_account_v2" %q {
%s  contact_email = %q
  dashboard     = "none"
  defaults      = { responsibilities = { fees_collector = "application", losses_collector = "application" } }
  identity = {
    country     = "US"
    entity_type = %q
    %s
  }
  configuration = { merchant = { capabilities = { card_payments = { requested = true } } } }
}
`, name, dependsOn, c.Name(name)+"@example.com", entity, identity)
}

// identityConfig creates the accounts one after another, each depending on the one before:
// Stripe refuses more than five account creates a second with a test key.
func identityConfig(t *testing.T, c acctest.Config, updated bool) string {
	out := c.ProviderBlock() + requirementFiles(t)
	previous := ""
	for _, name := range slices.Sorted(maps.Keys(identityChanges)) {
		ch := identityChanges[name]
		identity := ch.created
		if updated {
			identity = ch.updated
		}
		out += merchantWith(c, name, ch.entity, identity, previous)
		previous = name
	}
	return out
}

// Each member of the individual's identity, the company's business details and its attestations
// changes in place, as the suite's update probes change it on a merchant created with that member
// alone. The update sends the hash that holds it whole, as the probes do. A maiden name replaces
// an alias, and its full name goes with the alias, as `update/14` sent it. Removing the members
// then changes nothing the configuration still sets.
func TestAccAccountV2_identityInPlace(t *testing.T) {
	acct := acctest.NewAccount(t)
	check := func(updated bool) resource.TestCheckFunc {
		var checks []resource.TestCheckFunc
		for name, ch := range identityChanges {
			want := ch.before
			if updated {
				want = ch.after
			}
			checks = append(checks, resource.TestCheckResourceAttr("stripe_account_v2."+name, ch.attr, want))
		}
		return resource.ComposeAggregateTestCheckFunc(checks...)
	}
	var updates []plancheck.PlanCheck
	for name := range identityChanges {
		updates = append(updates, plancheck.ExpectResourceAction("stripe_account_v2."+name, plancheck.ResourceActionUpdate))
	}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config:           identityConfig(t, acct, false),
				Check:            check(false),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config: identityConfig(t, acct, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					check(true),
					resource.TestCheckNoResourceAttr("stripe_account_v2.maiden_name", "identity.individual.additional_names.0.full_name"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             updates,
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		}, nil, "stripe_account_v2", "stripe_file"),
	})
}

// configurations are the suite's probes `update/4`, a merchant created with every setting of
// its configuration, and `update/6`, a customer created with every setting of its, each of
// which an update changes whole. Branding names business icon and logo uploads, which Stripe
// takes only 128 pixels square or larger, and only on an update: the record shows a create taking
// them with no effect (`accepted_ignored`), so the create here leaves them out. digits are the
// seven digits after the customer's invoice prefix letter.
func configurationsConfig(t *testing.T, c acctest.Config, digits string, updated bool) string {
	dir := t.TempDir()
	files := ""
	for _, f := range []struct{ name, purpose string }{
		{"icon_update", "business_icon"}, {"logo_update", "business_logo"},
	} {
		path := filepath.Join(dir, f.name+".png")
		if err := os.WriteFile(path, squarePNG(t, 128), 0o600); err != nil {
			t.Fatal(err)
		}
		files += fmt.Sprintf("\nresource \"stripe_file\" %q {\n  source  = %q\n  purpose = %q\n}\n", f.name, path, f.purpose)
	}
	merchant := `{
      branding = { primary_color = "#0a2540", secondary_color = "#635bff" }
      capabilities = { card_payments = { requested = true } }
      card_payments = { decline_on = { avs_failure = true, cvc_failure = true } }
      mcc = "5734"
      statement_descriptor = { descriptor = "ATTRS CONFORMANCE", prefix = "ATTRS" }
      support = {
        address = { city = "San Francisco", country = "US", line1 = "510 Townsend St", line2 = "Suite 100", postal_code = "94103", state = "CA", town = "Mission Bay" }
        email   = %q
        phone   = "+18008675313"
        url     = "https://accessible.stripe.com"
      }
    }`
	customer := `{
      automatic_indirect_tax = { exempt = "exempt", ip_address = "8.8.8.8" }
      billing = { invoice = {
        custom_fields = [{ name = "Reference", value = "PO-1" }]
        footer        = "Thank you"
        next_sequence = 7
        prefix        = "A` + digits + `"
        rendering     = { amount_tax_display = "exclude_tax" }
      } }
      capabilities = { automatic_indirect_tax = { requested = true } }
      shipping = {
        address = { city = "San Francisco", country = "US", line1 = "510 Townsend St", line2 = "Suite 100", postal_code = "94103", state = "CA" }
        name    = "Jenny Rosen"
        phone   = "+18008675315"
      }
    }`
	if updated {
		merchant = `{
      branding = { icon = stripe_file.icon_update.id, logo = stripe_file.logo_update.id, primary_color = "#425466", secondary_color = "#00d4ff" }
      capabilities = { card_payments = { requested = true } }
      card_payments = { decline_on = { avs_failure = false, cvc_failure = false } }
      mcc = "7372"
      statement_descriptor = { descriptor = "ATTRS SUITE", prefix = "SUITE" }
      support = {
        address = { city = "Seattle", country = "US", line1 = "400 Pine St", line2 = "Suite 200", postal_code = "98101", state = "WA", town = "Downtown" }
        email   = %q
        phone   = "+18008675314"
        url     = "https://www.iana.org"
      }
    }`
		customer = `{
      automatic_indirect_tax = { exempt = "none", ip_address = "1.1.1.1", validate_location = "auto" }
      billing = { invoice = {
        custom_fields = [{ name = "Purchase order", value = "PO-2" }]
        footer        = "Thanks again"
        next_sequence = 8
        prefix        = "B` + digits + `"
        rendering     = { amount_tax_display = "include_inclusive_tax" }
      } }
      capabilities = { automatic_indirect_tax = { requested = false } }
      shipping = {
        address = { city = "Seattle", country = "US", line1 = "400 Pine St", line2 = "Suite 200", postal_code = "98101", state = "WA" }
        name    = "Jane Roe"
        phone   = "+18008675316"
      }
    }`
	}
	return c.ProviderBlock() + files + fmt.Sprintf(`
resource "stripe_account_v2" "merchant" {
  contact_email = %q
  dashboard     = "none"
  defaults      = { responsibilities = { fees_collector = "application", losses_collector = "application" } }
  identity      = { country = "US" }
  configuration = { merchant = %s }
}

resource "stripe_account_v2" "customer" {
  contact_email = %q
  identity      = { country = "US" }
  configuration = { customer = %s }
}
`, c.Name("merchant")+"@example.com", fmt.Sprintf(merchant, c.Name("support")+"@example.com"), c.Name("customer")+"@example.com", customer)
}

// A merchant's and a customer's configuration take every setting on create, as the suite's
// probes `update/4` and `update/6` create them. An update of either would send it whole, with the
// capability it requests, and the record proves no update that requests a capability, so a plan
// that changes one fails. Stripe moves a merchant capability's status on its own, so only its
// form is checked.
func TestAccAccountV2_configurationsOnCreate(t *testing.T) {
	acct := acctest.NewAccount(t)
	// Stripe keeps an invoice prefix unique among an account's customers, so each run takes its
	// own, `A` or `B` and seven random digits, as the suite generates it (`$gen` `digits`).
	digits := fmt.Sprintf("%07d", rand.IntN(10_000_000))
	status := regexp.MustCompile(`^(active|pending|restricted|unsupported)$`)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: configurationsConfig(t, acct, digits, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("stripe_account_v2.merchant", "configuration.merchant.mcc", "5734"),
					resource.TestCheckResourceAttr("stripe_account_v2.merchant", "configuration.merchant.branding.primary_color", "#0a2540"),
					resource.TestCheckResourceAttr("stripe_account_v2.merchant", "configuration.merchant.statement_descriptor.prefix", "ATTRS"),
					resource.TestCheckResourceAttr("stripe_account_v2.merchant", "configuration.merchant.support.address.town", "Mission Bay"),
					resource.TestMatchResourceAttr("stripe_account_v2.merchant", "configuration.merchant.capabilities.card_payments.status", status),
					resource.TestCheckResourceAttr("stripe_account_v2.customer", "configuration.customer.billing.invoice.custom_fields.0.name", "Reference"),
					resource.TestCheckResourceAttr("stripe_account_v2.customer", "configuration.customer.billing.invoice.next_sequence", "7"),
					resource.TestCheckResourceAttr("stripe_account_v2.customer", "configuration.customer.billing.invoice.rendering.amount_tax_display", "exclude_tax"),
					resource.TestCheckResourceAttr("stripe_account_v2.customer", "configuration.customer.automatic_indirect_tax.exempt", "exempt"),
					resource.TestCheckResourceAttr("stripe_account_v2.customer", "configuration.customer.shipping.name", "Jenny Rosen"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config:      configurationsConfig(t, acct, digits, true),
				ExpectError: regexp.MustCompile("Changing\\s+`configuration\\.(customer|merchant)`"),
			},
		}, nil, "stripe_account_v2", "stripe_file"),
	})
}
