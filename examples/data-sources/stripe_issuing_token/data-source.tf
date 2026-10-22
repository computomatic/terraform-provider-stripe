variable "connected_account" {
  type        = string
  description = "The connected account the object lives on."
}

provider "stripe" {
  alias          = "connected"
  stripe_account = var.connected_account
}

data "stripe_issuing_token" "wallet" {
  provider = stripe.connected
  id       = "intok_1MvSieLkdIwHu7ixmbvSzRjL"
}

output "token_status" {
  value = data.stripe_issuing_token.wallet.status
}
