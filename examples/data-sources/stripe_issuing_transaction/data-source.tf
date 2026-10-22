variable "connected_account" {
  type        = string
  description = "The connected account the object lives on."
}

provider "stripe" {
  alias          = "connected"
  stripe_account = var.connected_account
}

data "stripe_issuing_transaction" "purchase" {
  provider = stripe.connected
  id       = "ipi_1MvSieLkdIwHu7ixdA4EgS3k"
}

output "transaction_amount" {
  value = data.stripe_issuing_transaction.purchase.amount
}
