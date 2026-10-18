package file_link_test

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const address = "stripe_file_link.test"

// seedFile puts a file in the account, which Terraform cannot upload.
func seedFile(t *testing.T, acct acctest.Config) string {
	t.Helper()
	return acctest.Seed(t, acct, wire.Object{
		{Name: "object", Value: wire.String("file")},
		{Name: "filename", Value: wire.String("report.png")},
		{Name: "purpose", Value: wire.String("dispute_evidence")},
		{Name: "size", Value: wire.Int(70)},
		{Name: "type", Value: wire.String("png")},
		{Name: "title", Value: wire.Null{}},
		{Name: "url", Value: wire.Null{}},
		{Name: "expires_at", Value: wire.Null{}},
	})[0]
}

func inOneDay(days int) string {
	return strconv.FormatInt(time.Now().Add(time.Duration(days)*24*time.Hour).Unix(), 10)
}

// linkConfig is a file link. extra lands inside the resource block.
func linkConfig(c acctest.Config, file string, extra string) string {
	return c.ProviderBlock() + fmt.Sprintf(`
resource "stripe_file_link" "test" {
  file = %q
%s}
`, file, extra)
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

func TestAccFileLink_create(t *testing.T) {
	acct := acctest.NewAccount(t)
	file := seedFile(t, acct)
	expires := inOneDay(1)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: linkConfig(acct, file, fmt.Sprintf("  expires_at = %q\n  metadata   = { team = \"a\" }\n", expires)) + `
data "stripe_file_link" "test" {
  id = stripe_file_link.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(address, "id"),
				resource.TestCheckResourceAttr(address, "file", file),
				resource.TestCheckResourceAttr(address, "expires_at", expires),
				resource.TestCheckResourceAttr(address, "expired", "false"),
				resource.TestCheckResourceAttrSet(address, "url"),
				resource.TestCheckResourceAttr(address, "metadata.team", "a"),
				resource.TestCheckResourceAttrPair("data.stripe_file_link.test", "id", address, "id"),
				resource.TestCheckResourceAttr("data.stripe_file_link.test", "file", file),
			),
		}},
	})
}

func TestAccFileLink_update(t *testing.T) {
	acct := acctest.NewAccount(t)
	file := seedFile(t, acct)
	var first string
	later := inOneDay(2)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: linkConfig(acct, file, fmt.Sprintf("  expires_at = %q\n  metadata = { team = \"a\", tier = \"1\" }\n", inOneDay(1))), Check: idOf(&first)},
			{
				Config: linkConfig(acct, file, fmt.Sprintf("  expires_at = %q\n  metadata = { team = \"b\" }\n", later)),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "expires_at", later),
					resource.TestCheckResourceAttr(address, "metadata.%", "1"),
					resource.TestCheckResourceAttr(address, "metadata.team", "b"),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v != first {
							return fmt.Errorf("the in-place update changed the ID from %s to %s", first, v)
						}
						return nil
					}),
				),
			},
		},
	})
}

// file is accepted on create only, so another file replaces the link.
func TestAccFileLink_replace(t *testing.T) {
	acct := acctest.NewAccount(t)
	one, two := seedFile(t, acct), seedFile(t, acct)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: linkConfig(acct, one, ""), Check: idOf(&first)},
			{
				Config: linkConfig(acct, two, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "file", two),
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v == first {
							return fmt.Errorf("the replacement kept the link ID %s", v)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestAccFileLink_import(t *testing.T) {
	acct := acctest.NewAccount(t)
	cfg := linkConfig(acct, seedFile(t, acct), fmt.Sprintf("  expires_at = %q\n  metadata = { team = \"a\" }\n", inOneDay(1)))
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
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
		},
	})
}

// Stripe cannot delete a file link (suite: file_link_test.go, "cannot be deleted"), so
// destroy expires it with `expires_at` set to now ("update expires now") and the link
// stays in the account.
func TestAccFileLink_delete(t *testing.T) {
	acct := acctest.NewAccount(t)
	file := seedFile(t, acct)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			resp, err := tfacc.Client(acct).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/file_links/" + id})
			if err != nil {
				return fmt.Errorf("an expired file link still retrieves: %w", err)
			}
			if expired, _ := resp.Body.(wire.Object).GetBool("expired"); !expired {
				return fmt.Errorf("file link %s should be expired after destroy", id)
			}
			return nil
		},
		Steps: []resource.TestStep{{Config: linkConfig(acct, file, fmt.Sprintf("  expires_at = %q\n", inOneDay(1))), Check: idOf(&id)}},
	})
}

func TestAccFileLink_drift(t *testing.T) {
	acct := acctest.NewAccount(t)
	file := seedFile(t, acct)
	var id string
	cfg := linkConfig(acct, file, "  metadata = { team = \"a\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, acct, "/v1/file_links/"+id, wire.Object{
						{Name: "metadata", Value: wire.Object{{Name: "team", Value: wire.String("changed")}}},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(address, "metadata.team", "a"),
			},
		},
	})
}
