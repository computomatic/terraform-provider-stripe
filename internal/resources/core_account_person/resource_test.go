package core_account_person_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/go-version"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const (
	address = "stripe_core_account_person.test"
	second  = "stripe_core_account_person.second"
)

// accountBlock is a v2 merchant account, as the suite's persons live on (suite:
// "TestV2CoreAccountPerson"). Stripe cannot delete one, so every test forgets it.
func accountBlock(c acctest.Config, name string) string {
	return fmt.Sprintf(`
resource "stripe_core_account" %q {
  display_name = %q
  dashboard    = "none"
  identity = {
    country          = "us"
    entity_type      = "company"
    business_details = { registered_name = %q }
  }
  configuration = {
    merchant = { capabilities = { card_payments = { requested = true } } }
  }
  defaults = {
    currency         = "usd"
    responsibilities = { fees_collector = "application", losses_collector = "application" }
  }
}
`, name, c.Name("persons "+name), c.Name("Persons "+name+" Inc."))
}

// personBlock is a US person with the values Stripe's Connect testing guide documents as
// always verifying, the country in lower case as the suite sends it. line2 and extra land in
// the address and the person.
func personBlock(c acctest.Config, name, account, given, line2, extra string) string {
	if line2 != "" {
		line2 = fmt.Sprintf("    line2       = %q\n", line2)
	}
	return fmt.Sprintf(`
resource "stripe_core_account_person" %q {
  account_id = stripe_core_account.%s.id
  given_name = %q
  surname    = "Rosen"
  email      = "jenny.rosen@example.com"
  phone      = "0000000000"

  date_of_birth = {
    day   = 1
    month = 1
    year  = 1901
  }

  address = {
    line1       = "address_full_match"
%s    city        = "South San Francisco"
    state       = "CA"
    postal_code = "94080"
    country     = "us"
  }
%s}
`, name, account, c.Name(given), line2, extra)
}

const representative = `
  id_numbers = [{ type = "us_ssn_last_4", value = "0000" }]
  relationship = {
    representative = true
    executive      = true
    title          = "CEO"
  }
`

func config(c acctest.Config, extra string) string {
	return c.ProviderBlock() + accountBlock(c, "test") + personBlock(c, "test", "test", "Jenny", "", extra)
}

// importID is the account ID and the person ID, as the import ID wants them.
func importID(s *terraform.State) (string, error) {
	rs, ok := s.RootModule().Resources[address]
	if !ok {
		return "", fmt.Errorf("%s is not in state", address)
	}
	return rs.Primary.Attributes["account_id"] + "/" + rs.Primary.ID, nil
}

// ids records the account and person IDs of the resource at addr.
func ids(addr string, account, id *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[addr]
		if !ok {
			return fmt.Errorf("%s is not in state", addr)
		}
		*account, *id = rs.Primary.Attributes["account_id"], rs.Primary.ID
		return nil
	}
}

func personPath(account, id string) string {
	return "/v2/core/accounts/" + account + "/persons/" + id
}

func jsonIs(v wire.Value, want string) bool {
	if v == nil {
		return false
	}
	got, err := wire.EncodeJSON(v)
	return err == nil && string(got) == want
}

// Stripe lists an ID number's type without its value and shows the country in upper case
// (suite: "create representative"); state keeps what was configured, and the data source
// shows Stripe's.
func TestAccCoreAccountPerson_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: config(acct, representative+"  metadata = { k = \"v\" }\n") + `
data "stripe_core_account_person" "test" {
  account_id = stripe_core_account.test.id
  id         = stripe_core_account_person.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestMatchResourceAttr(address, "id", regexp.MustCompile(`^person_`)),
				resource.TestCheckResourceAttrPair(address, "account_id", "stripe_core_account.test", "id"),
				resource.TestCheckResourceAttr(address, "given_name", acct.Name("Jenny")),
				resource.TestMatchResourceAttr(address, "created", regexp.MustCompile(`^\d{4}-\d\d-\d\dT`)),
				resource.TestMatchResourceAttr(address, "updated", regexp.MustCompile(`^\d{4}-\d\d-\d\dT`)),
				resource.TestCheckResourceAttr(address, "date_of_birth.year", "1901"),
				resource.TestCheckResourceAttr(address, "address.country", "us"),
				resource.TestCheckResourceAttr(address, "relationship.representative", "true"),
				resource.TestCheckResourceAttr(address, "relationship.title", "CEO"),
				resource.TestCheckResourceAttr(address, "id_numbers.#", "1"),
				resource.TestCheckResourceAttr(address, "id_numbers.0.type", "us_ssn_last_4"),
				resource.TestCheckResourceAttr(address, "id_numbers.0.value", "0000"),
				resource.TestCheckResourceAttr(address, "metadata.k", "v"),
				resource.TestCheckResourceAttr(address, "livemode", "false"),
				resource.TestCheckResourceAttrPair("data.stripe_core_account_person.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_core_account_person.test", "address.country", "US"),
				resource.TestCheckResourceAttr("data.stripe_core_account_person.test", "id_numbers.0.type", "us_ssn_last_4"),
				resource.TestCheckNoResourceAttr("data.stripe_core_account_person.test", "id_numbers.0.value"),
			),
		}}, nil, "stripe_core_account"),
	})
}

// Stripe merges `relationship`, `address` and `metadata` and removes a metadata key sent as
// null (suite: "update relationship address and metadata", "update unsets metadata key with
// null"); the engine sends each of the first two whole, and never the ID numbers. A whole
// percent ownership comes back with `.0`, and state keeps the configured one. Removing the
// phone fails, because nothing proves Stripe clears it with null.
func TestAccCoreAccountPerson_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	var account, id string
	base := acct.ProviderBlock() + accountBlock(acct, "test")
	owner := func(line2, extra string) string {
		return base + personBlock(acct, "test", "test", "Owen", line2, extra)
	}
	updated := `
  relationship = {
    owner             = true
    director          = true
    percent_ownership = "40.5"
    title             = "Chair"
  }
  metadata = { keep = "me", stage = "updated" }
`
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: owner("", "  relationship = {\n    owner             = true\n    percent_ownership = \"60\"\n  }\n  metadata = { keep = \"me\" }\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					ids(address, &account, &id),
					resource.TestCheckResourceAttr(address, "relationship.percent_ownership", "60"),
				),
			},
			{
				Config: owner("Suite 1", updated),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPtr(address, "id", &id),
					resource.TestCheckResourceAttr(address, "relationship.director", "true"),
					resource.TestCheckResourceAttr(address, "relationship.owner", "true"),
					resource.TestCheckResourceAttr(address, "relationship.percent_ownership", "40.5"),
					resource.TestCheckResourceAttr(address, "relationship.title", "Chair"),
					resource.TestCheckResourceAttr(address, "address.line2", "Suite 1"),
					resource.TestCheckResourceAttr(address, "metadata.stage", "updated"),
				),
			},
			{
				Config: owner("Suite 1", regexp.MustCompile(`, stage = "updated"`).ReplaceAllString(updated, "")),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(address, "metadata.stage"),
					resource.TestCheckResourceAttr(address, "metadata.keep", "me"),
					func(*terraform.State) error {
						if acct.Smoke {
							return nil
						}
						var updates []wire.Object
						log := acctest.RequestLog(t, acct)
						for i := len(log) - 1; i >= 0; i-- {
							if params, ok := log[i].Params.(wire.Object); ok && log[i].Method == "POST" && log[i].Path == personPath(account, id) {
								updates = append(updates, params)
							}
						}
						if len(updates) != 2 {
							return fmt.Errorf("want 2 updates, got %d", len(updates))
						}
						first := updates[0]
						if rel, _ := first.Get("relationship"); !jsonIs(rel, `{"director":true,"owner":true,"percent_ownership":"40.5","title":"Chair"}`) {
							return fmt.Errorf("the update must send the whole relationship: %+v", rel)
						}
						if addr, _ := first.Get("address"); !jsonIs(addr, `{"city":"South San Francisco","country":"us","line1":"address_full_match","line2":"Suite 1","postal_code":"94080","state":"CA"}`) {
							return fmt.Errorf("the update must send the whole address: %+v", addr)
						}
						for _, params := range updates {
							if _, sent := params.Get("id_numbers"); sent {
								return fmt.Errorf("an update sent id_numbers: %+v", params)
							}
						}
						if meta, _ := updates[1].Get("metadata"); !jsonIs(meta, `{"stage":null}`) {
							return fmt.Errorf("the removed metadata key must be sent as null: %+v", meta)
						}
						return nil
					},
				),
			},
			{
				Config:      regexp.MustCompile(`  phone      = "0000000000"\n`).ReplaceAllString(owner("Suite 1", updated), ""),
				ExpectError: regexp.MustCompile("clearing `phone` is not supported"),
			},
		}, nil, "stripe_core_account"),
	})
}

// A change to the ID numbers is refused at plan time, because the suite sets them on create
// only.
func TestAccCoreAccountPerson_refuseIDNumbers(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: config(acct, representative)},
			{
				Config:      config(acct, regexp.MustCompile(`"0000"`).ReplaceAllString(representative, `"1111"`)),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)Changing .id_numbers. is not supported yet.*taint`),
			},
		}, nil, "stripe_core_account"),
	})
}

// The person lives under its account, so moving it to another account creates a new person
// there and deletes the old one.
func TestAccCoreAccountPerson_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var account, id string
	both := acct.ProviderBlock() + accountBlock(acct, "test") + accountBlock(acct, "other")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: both + personBlock(acct, "test", "test", "Jenny", "", ""), Check: ids(address, &account, &id)},
			{
				Config: both + personBlock(acct, "test", "other", "Jenny", "", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionDestroyBeforeCreate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair(address, "account_id", "stripe_core_account.other", "id"),
					func(*terraform.State) error {
						_, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: personPath(account, id)})
						if !stripeapi.IsNotFound(err) {
							return fmt.Errorf("the replaced person %s should be deleted, GET gave %v", id, err)
						}
						return nil
					},
				),
			},
		}, nil, "stripe_core_account"),
	})
}

// The import ID is `<account_id>/<person_id>`. Stripe never returns an ID number's value and
// shows the country in upper case, so an import has neither the value nor the configured
// country.
func TestAccCoreAccountPerson_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	extra := representative + "  metadata = { k = \"v\" }\n"
	cfg := config(acct, extra)
	imported := config(acct, extra+"  lifecycle {\n    ignore_changes = [id_numbers, address.country]\n  }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:            address,
				ImportState:             true,
				ImportStateIdFunc:       importID,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"id_numbers.0.value", "address.country"},
			},
			{
				ResourceName:      address,
				ImportState:       true,
				ImportStateKind:   resource.ImportBlockWithID,
				ImportStateIdFunc: importID,
				Config:            imported,
			},
			{
				ResourceName:  address,
				ImportState:   true,
				ImportStateId: "not-a-compound-id",
				ExpectError:   regexp.MustCompile(`expected .<account_id id>/<id>.`),
			},
		}, nil, "stripe_core_account"),
	})
}

// The identity form needs Terraform or OpenTofu 1.12.0 or later. The identity carries the
// account as well as the person ID.
func TestAccCoreAccountPerson_importIdentity(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := config(acct, representative+"  lifecycle {\n    ignore_changes = [id_numbers, address.country]\n  }\n")
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
		}, nil, "stripe_core_account"),
	})
}

// Stripe deletes a person with DELETE and answers a later GET with a 404 (suite: "delete").
func TestAccCoreAccountPerson_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var account, id string
	one := config(acct, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: one + personBlock(acct, "second", "test", "Dana", "", ""), Check: ids(second, &account, &id)},
			{
				Config: one,
				Check: func(*terraform.State) error {
					_, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: personPath(account, id)})
					if !stripeapi.IsNotFound(err) {
						return fmt.Errorf("person %s should be deleted, GET gave %v", id, err)
					}
					return nil
				},
			},
		}, nil, "stripe_core_account"),
	})
}

func TestAccCoreAccountPerson_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var account, id string
	cfg := config(acct, representative)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: ids(address, &account, &id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, personPath(account, id), wire.Object{{Name: "given_name", Value: wire.String("Renamed")}})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "given_name", acct.Name("Jenny")),
			},
		}, nil, "stripe_core_account"),
	})
}
