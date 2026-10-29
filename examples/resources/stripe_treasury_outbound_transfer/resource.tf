variable "treasury_account" {
  type        = string
  description = "A connected account with the treasury capability active."
}

variable "financial_account" {
  type        = string
  description = "A financial account of the connected account."
}

# Treasury runs on a connected account, so a second provider configuration acts on it.
provider "stripe" {
  alias          = "treasury"
  stripe_account = var.treasury_account
}

variable "savings_account" {
  type        = string
  description = "Another financial account of the same connected account."
}

resource "stripe_treasury_outbound_transfer" "to_savings" {
  provider          = stripe.treasury
  financial_account = var.financial_account
  amount            = 5000
  currency          = "usd"

  destination_payment_method_data = {
    type              = "financial_account"
    financial_account = var.savings_account
  }
}

# Stripe posts a transfer between financial accounts at once.
output "outbound_transfer_status" {
  value = stripe_treasury_outbound_transfer.to_savings.status
}
