variable "connected_account" {
  type        = string
  description = "The connected account the object lives on."
}

provider "stripe" {
  alias          = "connected"
  stripe_account = var.connected_account
}

data "stripe_treasury_transaction" "deposit" {
  provider = stripe.connected
  id       = "trxn_1MtYCCLkdIwHu7ixgXzLiVq8"
}

output "transaction_flow_type" {
  value = data.stripe_treasury_transaction.deposit.flow_type
}
