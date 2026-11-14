package charge_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/acctest/tfacc"
)

// The legacy `destination`, as an account ID or as a hash, is Stripe's to refuse for a connected
// account with `transfers` and without `card_payments`, which the plan cannot see, and the apply
// passes its refusal on (suite: TestChargeAttributes, probes `isolated/destination_string`,
// `isolated/destination_account` and `isolated/destination_amount`).
func TestAccCharge_destinationRefused(t *testing.T) {
	acct := acctest.NewAccount(t)
	account := tfacc.TransfersAccount(t, acct)
	refused := regexp.MustCompile(`use\s+transfer_data\[destination\]`)
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: tfacc.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: chargeConfig(acct, 500, fmt.Sprintf("  destination = %q\n", account)), ExpectError: refused},
			{Config: chargeConfig(acct, 500, fmt.Sprintf("  destination_data = { account = %q }\n", account)), ExpectError: refused},
			{Config: chargeConfig(acct, 500, fmt.Sprintf("  destination_data = { account = %q, amount = 100 }\n", account)), ExpectError: refused},
		},
	})
}
