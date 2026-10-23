package account_person_test

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
	address = "stripe_account_person.test"
	second  = "stripe_account_person.second"
)

// accountBlock is the connected account the persons belong to, named for the test.
func accountBlock(c acctest.Config, name, run string) string {
	return fmt.Sprintf(`
resource "stripe_account" %q {
  type     = "custom"
  country  = "US"
  metadata = { run = %q }
}
`, name, c.Name(run))
}

// personBlock is a US person with the values Stripe's Connect testing guide documents as
// always verifying (suite: "create representative"). extra lands inside it.
func personBlock(c acctest.Config, name, account, first, extra string) string {
	return personWithLine2(c, name, account, first, "", extra)
}

// personWithLine2 is personBlock with a second address line, unless line2 is empty.
func personWithLine2(c acctest.Config, name, account, first, line2, extra string) string {
	if line2 != "" {
		line2 = fmt.Sprintf("    line2       = %q\n", line2)
	}
	return fmt.Sprintf(`
resource "stripe_account_person" %q {
  account    = stripe_account.%s.id
  first_name = %q
  last_name  = "Rosen"
  email      = "jenny.rosen@example.com"
  phone      = "0000000000"
  id_number  = "000000000"

  dob = {
    day   = 1
    month = 1
    year  = 1901
  }

  address = {
    line1       = "address_full_match"
%s    city        = "South San Francisco"
    state       = "CA"
    postal_code = "94080"
    country     = "US"
  }
%s}
`, name, account, c.Name(first), line2, extra)
}

const representative = `
  ssn_last_4 = "0000"
  relationship = {
    representative = true
    executive      = true
    title          = "CEO"
  }
`

func config(c acctest.Config, extra string) string {
	return c.ProviderBlock() + accountBlock(c, "test", "persons") + personBlock(c, "test", "test", "Jenny", extra)
}

// importID is the account ID and the person ID, as the import ID wants them.
func importID(s *terraform.State) (string, error) {
	rs, ok := s.RootModule().Resources[address]
	if !ok {
		return "", fmt.Errorf("%s is not in state", address)
	}
	return rs.Primary.Attributes["account"] + "/" + rs.Primary.ID, nil
}

// ids records the account and person IDs of the resource at addr.
func ids(addr string, account, id *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[addr]
		if !ok {
			return fmt.Errorf("%s is not in state", addr)
		}
		*account, *id = rs.Primary.Attributes["account"], rs.Primary.ID
		return nil
	}
}

func personPath(account, id string) string {
	return "/v1/accounts/" + account + "/persons/" + id
}

// sent returns the form parameters of the fake's logged POSTs to path, oldest first.
func sent(t *testing.T, c acctest.Config, path string) []wire.Object {
	var out []wire.Object
	log := acctest.RequestLog(t, c)
	for i := len(log) - 1; i >= 0; i-- {
		if params, ok := log[i].Params.(wire.Object); ok && log[i].Method == "POST" && log[i].Path == path {
			out = append(out, params)
		}
	}
	return out
}

// Stripe never returns the identity numbers, only whether they were provided, and shows the
// phone number with +1 (suite: "create representative"); state keeps what was configured.
func TestAccAccountPerson_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config(acct, representative+"  metadata = { k = \"v\" }\n") + `
data "stripe_account_person" "test" {
  account = stripe_account.test.id
  id      = stripe_account_person.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestMatchResourceAttr(address, "id", regexp.MustCompile(`^person_`)),
				resource.TestCheckResourceAttrPair(address, "account", "stripe_account.test", "id"),
				resource.TestCheckResourceAttr(address, "first_name", acct.Name("Jenny")),
				resource.TestCheckResourceAttr(address, "dob.year", "1901"),
				resource.TestCheckResourceAttr(address, "address.line1", "address_full_match"),
				resource.TestCheckNoResourceAttr(address, "address.line2"),
				resource.TestCheckResourceAttr(address, "relationship.representative", "true"),
				resource.TestCheckResourceAttr(address, "relationship.executive", "true"),
				resource.TestCheckResourceAttr(address, "relationship.title", "CEO"),
				// Roles left out are Stripe's false; the percent ownership is null.
				resource.TestCheckResourceAttr(address, "relationship.owner", "false"),
				resource.TestCheckNoResourceAttr(address, "relationship.percent_ownership"),
				resource.TestCheckResourceAttr(address, "phone", "0000000000"),
				resource.TestCheckResourceAttr(address, "id_number", "000000000"),
				resource.TestCheckResourceAttr(address, "ssn_last_4", "0000"),
				resource.TestCheckResourceAttr(address, "id_number_provided", "true"),
				resource.TestCheckResourceAttr(address, "ssn_last_4_provided", "true"),
				resource.TestCheckResourceAttr(address, "metadata.k", "v"),
				resource.TestCheckResourceAttrSet(address, "verification.status"),
				resource.TestCheckResourceAttrPair("data.stripe_account_person.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_account_person.test", "relationship.title", "CEO"),
				resource.TestCheckResourceAttr("data.stripe_account_person.test", "phone", "+10000000000"),
				resource.TestCheckNoResourceAttr("data.stripe_account_person.test", "id_number"),
				resource.TestCheckNoResourceAttr("data.stripe_account_person.test", "ssn_last_4"),
			),
		}},
	})
}

// Stripe merges `relationship`, `address` and `metadata` on update and removes a metadata key
// sent empty (suite: "update relationship address and metadata", "update unsets metadata
// key"). The identity numbers are never sent again.
func TestAccAccountPerson_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	var account, id string
	owner := `
  relationship = {
    owner             = true
    percent_ownership = 60
  }
  metadata = { stage = "created", keep = "me" }
`
	updated := `
  relationship = {
    owner             = true
    director          = true
    percent_ownership = 40.5
    title             = "Chair"
  }
  metadata = { stage = "updated", keep = "me" }
`
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(acct, owner),
				Check: resource.ComposeAggregateTestCheckFunc(
					ids(address, &account, &id),
					resource.TestCheckResourceAttr(address, "relationship.percent_ownership", "60"),
				),
			},
			{
				Config: acct.ProviderBlock() + accountBlock(acct, "test", "persons") + personWithLine2(acct, "test", "test", "Jenny", "Suite 1", updated),
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
			// Dropping a metadata key sends it empty; dropping the title leaves it on the person.
			{
				Config: config(acct, `
  relationship = {
    owner             = true
    director          = true
    percent_ownership = 40.5
  }
  metadata = { keep = "me" }
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(address, "metadata.stage"),
					resource.TestCheckResourceAttr(address, "metadata.keep", "me"),
					resource.TestCheckResourceAttr(address, "relationship.title", "Chair"),
					resource.TestCheckResourceAttr(address, "address.line2", "Suite 1"),
					func(*terraform.State) error {
						if acct.Smoke {
							return nil
						}
						updates := sent(t, acct, personPath(account, id))
						if len(updates) != 2 {
							return fmt.Errorf("want 2 updates, got %d", len(updates))
						}
						for _, params := range updates {
							for _, never := range []string{"id_number", "ssn_last_4", "first_name", "phone"} {
								if _, ok := params.Get(never); ok {
									return fmt.Errorf("an update sent the unchanged %s: %+v", never, params)
								}
							}
						}
						meta, _ := updates[1].GetObject("metadata")
						if v, ok := meta.GetString("stage"); !ok || v != "" {
							return fmt.Errorf("the removed metadata key must be sent empty: %+v", updates[1])
						}
						if _, ok := updates[1].Get("relationship"); ok {
							return fmt.Errorf("removing the title must send no relationship: %+v", updates[1])
						}
						return nil
					},
				),
			},
		},
	})
}

// A change to the identity number is refused at plan time, because the suite sets it on create
// only.
func TestAccAccountPerson_refuseIDNumber(t *testing.T) {
	acct := acctest.NewAccount(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config(acct, representative)},
			{
				Config:      config(acct, regexp.MustCompile(`"0000"`).ReplaceAllString(representative, `"1111"`)),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)Changing .ssn_last_4. is not supported yet.*taint`),
			},
		},
	})
}

// The person lives under its account, so moving it to another account creates a new person
// there and deletes the old one.
func TestAccAccountPerson_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	var account, id string
	both := acct.ProviderBlock() + accountBlock(acct, "test", "persons") + accountBlock(acct, "other", "other persons")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: both + personBlock(acct, "test", "test", "Jenny", ""), Check: ids(address, &account, &id)},
			{
				Config: both + personBlock(acct, "test", "other", "Jenny", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionDestroyBeforeCreate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair(address, "account", "stripe_account.other", "id"),
					func(*terraform.State) error {
						_, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: personPath(account, id)})
						if !stripeapi.IsNotFound(err) {
							return fmt.Errorf("the replaced person %s should be deleted, GET gave %v", id, err)
						}
						return nil
					},
				),
			},
		},
	})
}

// The import ID is `<account_id>/<person_id>`. Stripe never returns the identity numbers and
// formats the phone number, so an import has none of the first and Stripe's form of the second.
func TestAccAccountPerson_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	extra := representative + "  metadata = { k = \"v\" }\n"
	cfg := config(acct, extra)
	imported := config(acct, extra+"  lifecycle {\n    ignore_changes = [id_number, ssn_last_4, phone]\n  }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:            address,
				ImportState:             true,
				ImportStateIdFunc:       importID,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"id_number", "ssn_last_4", "phone"},
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
				ExpectError:   regexp.MustCompile(`expected .<account id>/<id>.`),
			},
		},
	})
}

// The identity form needs Terraform or OpenTofu 1.12.0 or later. The identity carries the
// account as well as the person ID.
func TestAccAccountPerson_importIdentity(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := config(acct, representative+"  lifecycle {\n    ignore_changes = [id_number, ssn_last_4, phone]\n  }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.RequireAbove(version.Must(version.NewVersion("1.12.0")))},
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:    address,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
				Config:          cfg,
			},
		},
	})
}

// Stripe deletes a person with DELETE and answers a later GET with a 404 (suite: "delete").
func TestAccAccountPerson_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	var account, id string
	one := acct.ProviderBlock() + accountBlock(acct, "test", "persons") + personBlock(acct, "test", "test", "Jenny", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: one + personBlock(acct, "second", "test", "Dana", ""), Check: ids(second, &account, &id)},
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
		},
	})
}

func TestAccAccountPerson_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	var account, id string
	cfg := config(acct, representative)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: ids(address, &account, &id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, personPath(account, id), wire.Object{
						{Name: "relationship", Value: wire.Object{{Name: "title", Value: wire.String("Chair")}}},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "relationship.title", "CEO"),
			},
		},
	})
}

// When the account is deleted out of band, Stripe answers its persons with 403
// `account_invalid`. Refresh takes that for gone, so both are created again.
func TestAccAccountPerson_driftAccountDeleted(t *testing.T) {
	acct := acctest.NewAccount(t)
	var account, id string
	cfg := config(acct, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: ids(address, &account, &id)},
			{
				PreConfig: func() {
					if _, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "DELETE", Path: "/v1/accounts/" + account}); err != nil {
						t.Fatal(err)
					}
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction("stripe_account.test", plancheck.ResourceActionCreate),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionCreate),
				}},
			},
		},
	})
}
