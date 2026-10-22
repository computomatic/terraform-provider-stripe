variable "connected_account" {
  type        = string
  description = "The connected account the object lives on."
}

provider "stripe" {
  alias          = "connected"
  stripe_account = var.connected_account
}

data "stripe_treasury_transaction_entry" "deposit" {
  provider = stripe.connected
  id       = "trxne_1MtYCCLkdIwHu7ixa1Ii6ckh"
}

output "entry_balance_impact" {
  value = data.stripe_treasury_transaction_entry.deposit.balance_impact.cash
}
