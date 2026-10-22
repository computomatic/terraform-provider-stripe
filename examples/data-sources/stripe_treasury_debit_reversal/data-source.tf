variable "connected_account" {
  type        = string
  description = "The connected account the object lives on."
}

provider "stripe" {
  alias          = "connected"
  stripe_account = var.connected_account
}

data "stripe_treasury_debit_reversal" "disputed" {
  provider = stripe.connected
  id       = "debrev_1MtYCCLkdIwHu7ixA8MgJkCh"
}

output "debit_reversal_status" {
  value = data.stripe_treasury_debit_reversal.disputed.status
}
