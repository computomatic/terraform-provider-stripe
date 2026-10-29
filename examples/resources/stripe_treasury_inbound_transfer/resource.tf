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

variable "bank_account" {
  type        = string
  description = "A US bank account payment method attached to the connected account itself for inbound flows, by the Dashboard or a setup intent with attach_to_self."
}

resource "stripe_treasury_inbound_transfer" "top_up" {
  provider              = stripe.treasury
  financial_account     = var.financial_account
  amount                = 10000
  currency              = "usd"
  origin_payment_method = var.bank_account
  description           = "Weekly top-up"
}

# Set status = "canceled" to cancel the transfer while it is still processing.
output "inbound_status" {
  value = stripe_treasury_inbound_transfer.top_up.status
}
