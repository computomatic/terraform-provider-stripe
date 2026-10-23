package issuing_dispute_test

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"testing"

	"github.com/hashicorp/go-version"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func TestMain(m *testing.M) { os.Exit(acctest.Main(m)) }

const (
	address = "stripe_issuing_dispute.test"
	// captured is the amount, in cents, of each transaction a test disputes.
	captured = 500
)

// scope is a test's account, its connected account with Issuing active and a funded, active
// card there whose captured transactions the tests dispute.
type scope struct {
	acct, conn acctest.Config
	card       string
}

func issuing(t *testing.T) scope {
	t.Helper()
	acct := acctest.NewAccount(t)
	conn := tfacc.IssuingAccount(t, acct)
	card, _ := tfacc.ActiveCard(t, conn, tfacc.IssuingFinancialAccount(t, conn))
	return scope{acct: acct, conn: conn, card: card}
}

// transaction captures a new authorization on the scope's card and returns the capture.
func (s scope) transaction(t *testing.T) string {
	t.Helper()
	_, txn := tfacc.CapturedTransaction(t, s.conn, s.card, captured)
	return txn
}

// disputeConfig disputes the transaction as fraudulent with explanation, or with the reason
// alone when explanation is empty. extra lands inside the dispute.
func disputeConfig(s scope, transaction, explanation, extra string) string {
	evidence := `{ reason = "fraudulent" }`
	if explanation != "" {
		evidence = fmt.Sprintf("{\n    reason     = \"fraudulent\"\n    fraudulent = { explanation = %q }\n  }", explanation)
	}
	return s.acct.ProviderBlock() + s.conn.ConnectedProviderBlock() + fmt.Sprintf(`
resource "stripe_issuing_dispute" "test" {
  provider    = stripe.connected
  transaction = %q
  evidence    = %s
%s}
`, transaction, evidence, extra)
}

// reasonConfig disputes the transaction with the reason alone.
func reasonConfig(s scope, transaction, reason string) string {
	return s.acct.ProviderBlock() + s.conn.ConnectedProviderBlock() + fmt.Sprintf(`
resource "stripe_issuing_dispute" "test" {
  provider    = stripe.connected
  transaction = %q
  evidence    = { reason = %q }
}
`, transaction, reason)
}

// otherReasons is the fake's answer to a reason other than `fraudulent`, the one it takes.
const otherReasons = "dispute reasons other than fraudulent"

// skipOtherReasons skips a test that changes a dispute to another reason before it creates
// one, when the fake does not take other reasons yet: a test that skipped later would leave a
// dispute its closing destroy refuses. The probe disputes a transaction of its own.
func skipOtherReasons(t *testing.T, s scope) {
	t.Helper()
	tfacc.SkipUnlessFakeServes(t, s.conn, "POST", "/v1/issuing/disputes", wire.Object{
		{Name: "transaction", Value: wire.String(s.transaction(t))},
		{Name: "evidence", Value: wire.Object{{Name: "reason", Value: wire.String("other")}}},
	})
}

func idOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = v
		return nil
	})
}

// pathOf records the dispute's API path.
func pathOf(dst *string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		*dst = "/v1/issuing/disputes/" + v
		return nil
	})
}

// A new dispute is unsubmitted, for the whole transaction, with the evidence given, and the
// transaction names it (suite: "create", "the transaction names the dispute").
func TestAccIssuingDispute_create(t *testing.T) {
	s := issuing(t)
	txn := s.transaction(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: disputeConfig(s, txn, "The cardholder did not make this purchase.", "  metadata = { env = \"test\" }\n") + `
data "stripe_issuing_dispute" "test" {
  provider = stripe.connected
  id       = stripe_issuing_dispute.test.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestMatchResourceAttr(address, "id", regexp.MustCompile(`^idp_`)),
				resource.TestCheckResourceAttr(address, "transaction", txn),
				resource.TestCheckResourceAttr(address, "status", "unsubmitted"),
				resource.TestCheckResourceAttr(address, "amount", fmt.Sprint(captured)),
				resource.TestCheckResourceAttr(address, "currency", "usd"),
				resource.TestCheckResourceAttr(address, "evidence.reason", "fraudulent"),
				resource.TestCheckResourceAttr(address, "evidence.fraudulent.explanation", "The cardholder did not make this purchase."),
				resource.TestCheckNoResourceAttr(address, "evidence.other.explanation"),
				resource.TestCheckResourceAttr(address, "metadata.env", "test"),
				resource.TestCheckResourceAttr(address, "livemode", "false"),
				resource.TestCheckResourceAttrSet(address, "created"),
				resource.TestCheckResourceAttrPair("data.stripe_issuing_dispute.test", "transaction", address, "transaction"),
				resource.TestCheckResourceAttrPair("data.stripe_issuing_dispute.test", "status", address, "status"),
				resource.TestCheckResourceAttrPair("data.stripe_issuing_dispute.test", "evidence.fraudulent.explanation", address, "evidence.fraudulent.explanation"),
				resource.TestCheckResourceAttrWith(address, "id", func(id string) error {
					resp, err := tfacc.Client(s.conn).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/issuing/transactions/" + txn})
					if err != nil {
						return err
					}
					if got, _ := resp.Body.(wire.Object).GetString("dispute"); got != id {
						return fmt.Errorf("transaction %s names dispute %q, want %s", txn, got, id)
					}
					return nil
				}),
			),
		}}, nil, "stripe_issuing_dispute"),
	})
}

// A dispute made with its reason alone and `status = "submitted"` is submitted on create, and
// Stripe fills in the reason's evidence with nulls (suite: "submit with a reason and no
// explanation"). Submitting reverses the capture's received debit on the Treasury-backed card.
func TestAccIssuingDispute_submitOnCreate(t *testing.T) {
	s := issuing(t)
	txn := s.transaction(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: disputeConfig(s, txn, "", "  status = \"submitted\"\n"),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "status", "submitted"),
				resource.TestCheckResourceAttr(address, "evidence.reason", "fraudulent"),
				resource.TestCheckNoResourceAttr(address, "evidence.fraudulent.explanation"),
				resource.TestCheckResourceAttrSet(address, "treasury.received_debit"),
				resource.TestCheckResourceAttrSet(address, "treasury.debit_reversal"),
			),
		}}, nil, "stripe_issuing_dispute"),
	})
}

// Stripe fills in the evidence hash of any reason the request names without it, as it does for
// `fraudulent` (suite: "submit with a reason and no explanation"), so a dispute with another
// reason alone reads back without a change.
func TestAccIssuingDispute_otherReason(t *testing.T) {
	s := issuing(t)
	txn := s.transaction(t)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		ErrorCheck:               tfacc.SkipWhenFakeLacks(t, otherReasons),
		Steps: acctest.Forget(t, []resource.TestStep{{
			Config: reasonConfig(s, txn, "other"),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "status", "unsubmitted"),
				resource.TestCheckResourceAttr(address, "evidence.reason", "other"),
				resource.TestCheckNoResourceAttr(address, "evidence.other.explanation"),
			),
		}}, nil, "stripe_issuing_dispute"),
	})
}

// A change of reason in place leaves the evidence hashes to Stripe's answer: the old reason's
// explanation is not planned from the state, and the new reason's hash is read back.
func TestAccIssuingDispute_changeReason(t *testing.T) {
	s := issuing(t)
	skipOtherReasons(t, s)
	txn := s.transaction(t)
	var first string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: disputeConfig(s, txn, "Not made by the cardholder.", ""), Check: idOf(&first)},
			{
				Config: reasonConfig(s, txn, "other"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
					plancheck.ExpectUnknownValue(address, tfjsonpath.New("evidence").AtMapKey("fraudulent")),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
						if v != first {
							return fmt.Errorf("the change of reason replaced the dispute %s with %s", first, v)
						}
						return nil
					}),
					resource.TestCheckResourceAttr(address, "evidence.reason", "other"),
					resource.TestCheckNoResourceAttr(address, "evidence.fraudulent.explanation"),
				),
			},
		}, nil, "stripe_issuing_dispute"),
	})
}

// Evidence, amount and metadata change in place while the dispute is unsubmitted; setting
// `status` submits it, after which metadata still changes and Stripe refuses new evidence
// (suite: "update", "submit", "metadata stays updatable", "evidence is fixed"). Stripe refuses
// `transaction` on update, so each update passing shows the provider left it out.
func TestAccIssuingDispute_update(t *testing.T) {
	s := issuing(t)
	txn := s.transaction(t)
	var first string
	inPlace := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
		plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
	}}
	sameDispute := resource.TestCheckResourceAttrWith(address, "id", func(v string) error {
		if v != first {
			return fmt.Errorf("the update replaced the dispute %s with %s", first, v)
		}
		return nil
	})
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{
				Config: disputeConfig(s, txn, "The cardholder did not make this purchase.", "  metadata = { a = \"1\" }\n"),
				Check:  idOf(&first),
			},
			{
				Config:           disputeConfig(s, txn, "Not made by the cardholder.", "  amount   = 400\n  metadata = { b = \"2\" }\n"),
				ConfigPlanChecks: inPlace,
				Check: resource.ComposeAggregateTestCheckFunc(
					sameDispute,
					resource.TestCheckResourceAttr(address, "status", "unsubmitted"),
					resource.TestCheckResourceAttr(address, "amount", "400"),
					resource.TestCheckResourceAttr(address, "evidence.fraudulent.explanation", "Not made by the cardholder."),
					resource.TestCheckNoResourceAttr(address, "metadata.a"),
					resource.TestCheckResourceAttr(address, "metadata.b", "2"),
				),
			},
			{
				Config:           disputeConfig(s, txn, "Not made by the cardholder.", "  amount   = 400\n  metadata = { b = \"2\" }\n  status   = \"submitted\"\n"),
				ConfigPlanChecks: inPlace,
				Check:            resource.ComposeAggregateTestCheckFunc(sameDispute, resource.TestCheckResourceAttr(address, "status", "submitted")),
			},
			{
				Config:           disputeConfig(s, txn, "Not made by the cardholder.", "  amount   = 400\n  metadata = { c = \"3\" }\n  status   = \"submitted\"\n"),
				ConfigPlanChecks: inPlace,
				Check: resource.ComposeAggregateTestCheckFunc(
					sameDispute,
					resource.TestCheckResourceAttr(address, "status", "submitted"),
					resource.TestCheckResourceAttr(address, "metadata.c", "3"),
				),
			},
			{
				Config:      disputeConfig(s, txn, "Changed after submitting.", "  amount   = 400\n  metadata = { c = \"3\" }\n  status   = \"submitted\"\n"),
				ExpectError: regexp.MustCompile(`Evidence can only be modified while the dispute is unsubmitted`),
			},
		}, nil, "stripe_issuing_dispute"),
	})
}

// submitted disputes the transaction as fraudulent with explanation and submits it. extra lands
// inside the dispute.
func submitted(s scope, transaction, explanation, extra string) string {
	return disputeConfig(s, transaction, explanation, "  status = \"submitted\"\n"+extra)
}

// awaitDecided waits until the dispute reads status, which Stripe decides about two seconds
// after the submit and the fake in it (suite: "Decided Issuing disputes in test mode").
func (s scope) awaitDecided(t *testing.T, id *string, status string) func() {
	return func() {
		tfacc.AwaitListed(t, s.conn, "/v1/issuing/disputes", wire.Object{{Name: "status", Value: wire.String(status)}}, func(items []wire.Object) bool {
			return slices.ContainsFunc(items, func(d wire.Object) bool { got, _ := d.GetString("id"); return got == *id })
		})
	}
}

// decided submits a dispute whose explanation decides it in test mode and proves that, once
// `status` reads the outcome, the configuration's `submitted` plans no change, and a change of
// metadata updates the dispute in place without submitting it again, which Stripe refuses
// (suite: "Decided Issuing disputes in test mode").
func decided(t *testing.T, explanation, outcome string) {
	s := issuing(t)
	txn := s.transaction(t)
	var id string
	cfg := submitted(s, txn, explanation, "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeAggregateTestCheckFunc(idOf(&id), resource.TestCheckResourceAttr(address, "status", "submitted"))},
			{
				PreConfig:        s.awaitDecided(t, &id, outcome),
				Config:           cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				Check:            resource.TestCheckResourceAttr(address, "status", outcome),
			},
			{
				Config: submitted(s, txn, explanation, "  metadata = { decided = \"yes\" }\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "status", outcome),
					resource.TestCheckResourceAttr(address, "metadata.decided", "yes"),
				),
			},
		}, nil, "stripe_issuing_dispute"),
	})
}

func TestAccIssuingDispute_won(t *testing.T) { decided(t, "winning_evidence", "won") }

func TestAccIssuingDispute_lost(t *testing.T) { decided(t, "losing_evidence", "lost") }

// A decided dispute cannot go back to `unsubmitted`, which no action sets, so the plan fails
// before anything reaches Stripe.
func TestAccIssuingDispute_unsubmitDecided(t *testing.T) {
	s := issuing(t)
	txn := s.transaction(t)
	var id string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: submitted(s, txn, "winning_evidence", ""), Check: idOf(&id)},
			{
				PreConfig:   s.awaitDecided(t, &id, "won"),
				Config:      disputeConfig(s, txn, "winning_evidence", "  status = \"unsubmitted\"\n"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("`status` can be set to `submitted`; got \"unsubmitted\""),
			},
		}, nil, "stripe_issuing_dispute"),
	})
}

// `transaction` is create-only, so changing it would replace the dispute, which Stripe cannot
// delete or withdraw: the plan fails, and so does a taint, at apply.
func TestAccIssuingDispute_replace(t *testing.T) {
	s := issuing(t)
	firstTxn, secondTxn := s.transaction(t), s.transaction(t)
	cfg := disputeConfig(s, firstTxn, "Not made by the cardholder.", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg},
			{Config: disputeConfig(s, secondTxn, "Not made by the cardholder.", ""), PlanOnly: true, ExpectError: tfacc.RefusedReplace},
			{Config: cfg, Taint: []string{address}, ExpectError: tfacc.RefusedDestroy},
		}, nil, "stripe_issuing_dispute"),
	})
}

func TestAccIssuingDispute_import(t *testing.T) {
	s := issuing(t)
	cfg := disputeConfig(s, s.transaction(t), "Not made by the cardholder.", "  metadata = { env = \"test\" }\n")
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
		}, nil, "stripe_issuing_dispute"),
	})
}

// The identity form needs Terraform or OpenTofu 1.12.0 or later.
func TestAccIssuingDispute_importIdentity(t *testing.T) {
	s := issuing(t)
	cfg := disputeConfig(s, s.transaction(t), "Not made by the cardholder.", "")
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
		}, nil, "stripe_issuing_dispute"),
	})
}

// Stripe cannot delete or withdraw a dispute, so dropping it from the configuration, and
// `terraform destroy`, fail at plan time, and the dispute stays unsubmitted.
func TestAccIssuingDispute_delete(t *testing.T) {
	s := issuing(t)
	var path string
	var mark int
	cfg := disputeConfig(s, s.transaction(t), "Not made by the cardholder.", "")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(pathOf(&path), tfacc.LogMark(t, s.conn, &mark))},
			{Config: acctest.Without(t, cfg, "stripe_issuing_dispute"), PlanOnly: true, ExpectError: tfacc.RefusedDestroy},
			{Config: cfg, Destroy: true, ExpectError: tfacc.RefusedDestroy},
		}, tfacc.Untouched(t, s.conn, &path, &mark, "status", wire.String("unsubmitted")), "stripe_issuing_dispute"),
	})
}

// A dispute whose evidence and metadata were changed out of band shows as a change, and the
// apply restores both.
func TestAccIssuingDispute_drift(t *testing.T) {
	s := issuing(t)
	var id string
	cfg := disputeConfig(s, s.transaction(t), "Not made by the cardholder.", "  metadata = { env = \"test\" }\n")
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: acctest.Forget(t, []resource.TestStep{
			{Config: cfg, Check: idOf(&id)},
			{
				PreConfig: func() {
					tfacc.Post(t, s.conn, "/v1/issuing/disputes/"+id, wire.Object{
						{Name: "metadata", Value: wire.Object{{Name: "env", Value: wire.String("drifted")}}},
						{Name: "evidence", Value: wire.Object{
							{Name: "reason", Value: wire.String("fraudulent")},
							{Name: "fraudulent", Value: wire.Object{{Name: "explanation", Value: wire.String("Changed out of band.")}}},
						}},
					})
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "metadata.env", "test"),
					resource.TestCheckResourceAttr(address, "evidence.fraudulent.explanation", "Not made by the cardholder."),
				),
			},
		}, nil, "stripe_issuing_dispute"),
	})
}
