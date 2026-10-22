variable "connected_account" {
  type        = string
  description = "The connected account the object lives on."
}

provider "stripe" {
  alias          = "connected"
  stripe_account = var.connected_account
}

data "stripe_issuing_card" "travel" {
  provider = stripe.connected
  id       = "ic_1MvSieLkdIwHu7ixn6uuO0Xu"
}

output "card_last4" {
  value = data.stripe_issuing_card.travel.last4
}
