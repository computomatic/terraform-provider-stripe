variable "treasury_account" {
  type        = string
  description = "A connected account with the treasury capability active."
}

variable "received_credit" {
  type        = string
  description = "An ACH received credit of one of the connected account's financial accounts."
}

# Treasury runs on a connected account, so a second provider configuration acts on it.
provider "stripe" {
  alias          = "treasury"
  stripe_account = var.treasury_account
}

resource "stripe_treasury_credit_reversal" "return_to_sender" {
  provider        = stripe.treasury
  received_credit = var.received_credit
  metadata = {
    reason = "sent in error"
  }
}
