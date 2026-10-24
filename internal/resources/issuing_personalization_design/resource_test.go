package issuing_personalization_design_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/go-version"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "stripe_issuing_personalization_design.test"

// A design Stripe has not rejected or ended is in review, or active should a sandbox approve it
// at once (suite: "create").
var unreviewed = regexp.MustCompile(`^(review|active)$`)

// scope is a test's account, its connected account with Issuing active, a physical bundle there
// that takes a card logo and carrier text, and a card logo file the harness uploaded.
type scope struct {
	acct, conn   acctest.Config
	bundle, logo string
}

func issuing(t *testing.T) scope {
	t.Helper()
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	return scope{acct: acct, conn: conn, bundle: tfacc.PhysicalBundle(t, conn), logo: tfacc.CardLogo(t, conn)}
}

// designConfig is a design on the scope's bundle with its logo. extra lands inside the design.
// A bundle may require carrier text (suite: "no carrier text on a bundle that requires it"),
// so every design that is created sets it.
func designConfig(s scope, extra string) string {
	return s.acct.ProviderBlock() + s.conn.ConnectedProviderBlock() + fmt.Sprintf(`
resource "stripe_issuing_personalization_design" "test" {
  provider        = stripe.connected
  physical_bundle = %q
  card_logo       = %q
%s}
`, s.bundle, s.logo, extra)
}

const carrierText = `  carrier_text = {
    header_title = "Welcome"
    header_body  = "Your new card is enclosed."
    footer_title = "Questions"
    footer_body  = "Contact support."
  }
`

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

// pathOf records the design's API path.
func pathOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = "/v1/issuing/personalization_designs/" + v
		return nil
	})
}

// helper moves a design through Stripe's review with a test helper, as the suite does
// ("activate and deactivate", "reject").
func helper(t *testing.T, conn acctest.Config, id, action string, params wire.Object) {
	t.Helper()
	tfacc.Post(t, conn, "/v1/test_helpers/issuing/personalization_designs/"+id+"/"+action, params)
}

// A new design is in review, with no rejection reasons, not a default, and echoes its bundle,
// logo, name, lookup key and carrier text (suite: "create"). The data source finds it by `id`
// and by `lookup_key` (suite: "list").
func TestAccIssuingPersonalizationDesign_create(t *testing.T) {
	s := issuing(t)
	key := s.acct.Unique("design_create")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: designConfig(s, fmt.Sprintf("  name       = %q\n  lookup_key = %q\n  metadata   = { env = \"test\" }\n", s.acct.Name("Standard card"), key)+carrierText) + `
data "stripe_issuing_personalization_design" "by_id" {
  provider = stripe.connected
  id       = stripe_issuing_personalization_design.test.id
}

data "stripe_issuing_personalization_design" "by_lookup_key" {
  provider   = stripe.connected
  lookup_key = stripe_issuing_personalization_design.test.lookup_key
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestMatchResourceAttr(address, "id", regexp.MustCompile(`^ipcd_`)),
				resource.TestCheckResourceAttr(address, "physical_bundle", s.bundle),
				resource.TestCheckResourceAttr(address, "card_logo", s.logo),
				resource.TestCheckResourceAttr(address, "name", s.acct.Name("Standard card")),
				resource.TestCheckResourceAttr(address, "lookup_key", key),
				resource.TestCheckResourceAttr(address, "metadata.env", "test"),
				resource.TestCheckResourceAttr(address, "carrier_text.header_title", "Welcome"),
				resource.TestCheckResourceAttr(address, "carrier_text.footer_body", "Contact support."),
				resource.TestMatchResourceAttr(address, "status", unreviewed),
				resource.TestCheckResourceAttr(address, "rejection_reasons.card_logo.#", "0"),
				resource.TestCheckResourceAttr(address, "rejection_reasons.carrier_text.#", "0"),
				resource.TestCheckResourceAttr(address, "preferences.is_default", "false"),
				resource.TestCheckResourceAttr(address, "preferences.is_platform_default", "false"),
				resource.TestCheckResourceAttr(address, "livemode", "false"),
				resource.TestCheckResourceAttrSet(address, "created"),
				resource.TestCheckResourceAttrPair("data.stripe_issuing_personalization_design.by_id", "lookup_key", address, "lookup_key"),
				resource.TestCheckResourceAttrPair("data.stripe_issuing_personalization_design.by_id", "card_logo", address, "card_logo"),
				resource.TestCheckResourceAttrPair("data.stripe_issuing_personalization_design.by_id", "carrier_text.header_body", address, "carrier_text.header_body"),
				resource.TestCheckResourceAttrPair("data.stripe_issuing_personalization_design.by_lookup_key", "id", address, "id"),
				resource.TestCheckResourceAttrPair("data.stripe_issuing_personalization_design.by_lookup_key", "physical_bundle", address, "physical_bundle"),
			),
		}}, nil, "stripe_issuing_personalization_design"),
	})
}

// The name, lookup key and metadata change in place, and removing the name clears it (suite:
// "update"). Nothing proves that Stripe changes a design's bundle, logo or carrier text, so a
// plan that does fails before Stripe is called.
func TestAccIssuingPersonalizationDesign_update(t *testing.T) {
	s := issuing(t)
	var first string
	inPlace := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
		plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
	}}
	sameDesign := resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		if v != first {
			return fmt.Errorf("the update replaced the design %s with %s", first, v)
		}
		return nil
	})
	firstKey, secondKey := s.acct.Unique("design_update_first"), s.acct.Unique("design_update_second")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: designConfig(s, fmt.Sprintf("  name       = %q\n  lookup_key = %q\n  metadata   = { a = \"1\" }\n", s.acct.Name("First design"), firstKey)+carrierText),
				Check:  idOf(&first),
			},
			{
				Config:           designConfig(s, fmt.Sprintf("  name       = %q\n  lookup_key = %q\n  metadata   = { b = \"2\" }\n", s.acct.Name("Renamed design"), secondKey)+carrierText),
				ConfigPlanChecks: inPlace,
				Check: resource.ComposeAggregateTestCheckFunc(
					sameDesign,
					resource.TestCheckResourceAttr(address, "name", s.acct.Name("Renamed design")),
					resource.TestCheckResourceAttr(address, "lookup_key", secondKey),
					resource.TestCheckNoResourceAttr(address, "metadata.a"),
					resource.TestCheckResourceAttr(address, "metadata.b", "2"),
				),
			},
			{
				Config:           designConfig(s, fmt.Sprintf("  lookup_key = %q\n", secondKey)+carrierText),
				ConfigPlanChecks: inPlace,
				Check: resource.ComposeAggregateTestCheckFunc(
					sameDesign,
					resource.TestCheckNoResourceAttr(address, "name"),
					resource.TestCheckNoResourceAttr(address, "metadata.b"),
				),
			},
			{
				Config:      designConfig(s, fmt.Sprintf("  lookup_key = %q\n", secondKey)+`  carrier_text = { header_title = "Hello" }`+"\n"),
				ExpectError: regexp.MustCompile("Changing `carrier_text` is not supported yet"),
			},
			{
				Config: s.acct.ProviderBlock() + s.conn.ConnectedProviderBlock() + fmt.Sprintf(`
resource "stripe_issuing_personalization_design" "test" {
  provider        = stripe.connected
  physical_bundle = %q
  card_logo       = %q
  lookup_key      = %q
%s}
`, tfacc.AlteredID(s.bundle), s.logo, secondKey, carrierText),
				ExpectError: regexp.MustCompile("Changing `physical_bundle` is not supported yet"),
			},
		}, nil, "stripe_issuing_personalization_design"),
	})
}

// Stripe reviews a design: a test helper stands in for the review and moves it from `review` to
// `active` and on to `inactive`, or to `rejected` with its reasons (suite: "activate and
// deactivate", "reject"). The provider reads each state without planning a change.
func TestAccIssuingPersonalizationDesign_review(t *testing.T) {
	s := issuing(t)
	var id, rejected string
	cfg := designConfig(s, carrierText) + fmt.Sprintf(`
resource "stripe_issuing_personalization_design" "rejected" {
  provider        = stripe.connected
  physical_bundle = %q
  card_logo       = %q
%s}
`, s.bundle, s.logo, carrierText)
	unchanged := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					idOf(&id),
					resource.TestCheckResourceAttrWith("stripe_issuing_personalization_design.rejected", "id", func(v string) error { rejected = v; return nil }),
					resource.TestCheckResourceAttr(address, "status", "review"),
				),
			},
			{
				PreConfig: func() {
					helper(t, s.conn, id, "activate", nil)
					helper(t, s.conn, rejected, "reject", wire.Object{{Name: "rejection_reasons", Value: wire.Object{
						{Name: "card_logo", Value: wire.Array{wire.String("other")}},
					}}})
				},
				Config:           cfg,
				ConfigPlanChecks: unchanged,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "status", "active"),
					resource.TestCheckResourceAttr("stripe_issuing_personalization_design.rejected", "status", "rejected"),
					resource.TestCheckResourceAttr("stripe_issuing_personalization_design.rejected", "rejection_reasons.card_logo.#", "1"),
					resource.TestCheckResourceAttr("stripe_issuing_personalization_design.rejected", "rejection_reasons.card_logo.0", "other"),
				),
			},
			{
				PreConfig:        func() { helper(t, s.conn, id, "deactivate", nil) },
				Config:           cfg,
				ConfigPlanChecks: unchanged,
				Check:            resource.TestCheckResourceAttr(address, "status", "inactive"),
			},
		}, nil, "stripe_issuing_personalization_design"),
	})
}

// A design has no field that forces replacement, but a taint plans one, and Stripe cannot
// delete a design: the apply fails before anything is created.
func TestAccIssuingPersonalizationDesign_replace(t *testing.T) {
	s := issuing(t)
	cfg := designConfig(s, carrierText)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: cfg, Taint: []string{address}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_issuing_personalization_design"),
	})
}

func TestAccIssuingPersonalizationDesign_import(t *testing.T) {
	s := issuing(t)
	cfg := designConfig(s, fmt.Sprintf("  name       = %q\n  lookup_key = %q\n  metadata   = { env = \"test\" }\n", s.acct.Name("Imported design"), s.acct.Unique("design_import"))+carrierText)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{
				Config:            cfg,
				ResourceName:      address,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:    address,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				Config:          cfg,
			},
		}, nil, "stripe_issuing_personalization_design"),
	})
}

// The identity form needs Terraform or OpenTofu 1.12.0 or later.
func TestAccIssuingPersonalizationDesign_importIdentity(t *testing.T) {
	s := issuing(t)
	cfg := designConfig(s, carrierText)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.RequireAbove(version.Must(version.NewVersion("1.12.0")))},
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:    address,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				Config:          cfg,
			},
		}, nil, "stripe_issuing_personalization_design"),
	})
}

// Stripe cannot delete a design (suite: "delete is not a route"), so dropping it from the
// configuration, and `terraform destroy`, fail at plan time, and the design stays as it was,
// with its lookup key.
func TestAccIssuingPersonalizationDesign_delete(t *testing.T) {
	s := issuing(t)
	var path string
	var mark int
	key := s.acct.Unique("design_delete")
	cfg := designConfig(s, fmt.Sprintf("  lookup_key = %q\n", key)+carrierText)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(pathOf(&path), tfacc.LogMark(t, s.conn, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_issuing_personalization_design"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, s.conn, &path, &mark, "lookup_key", wire.String(key)), "stripe_issuing_personalization_design"),
	})
}

// A design whose name and metadata were changed out of band shows as a change, and the apply
// restores both.
func TestAccIssuingPersonalizationDesign_drift(t *testing.T) {
	s := issuing(t)
	var id string
	cfg := designConfig(s, fmt.Sprintf("  name     = %q\n  metadata = { env = \"test\" }\n", s.acct.Name("Standard design"))+carrierText)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, s.conn, "/v1/issuing/personalization_designs/"+id, wire.Object{
						{Name: "name", Value: wire.String("Drifted")},
						{Name: "metadata", Value: wire.Object{{Name: "env", Value: wire.String("drifted")}}},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "name", s.acct.Name("Standard design")),
					resource.TestCheckResourceAttr(address, "metadata.env", "test"),
				),
			},
		}, nil, "stripe_issuing_personalization_design"),
	})
}
