variable "connected_account" {
  type        = string
  description = "The connected account the object lives on."
}

provider "stripe" {
  alias          = "connected"
  stripe_account = var.connected_account
}

data "stripe_issuing_physical_bundle" "standard" {
  provider = stripe.connected
  id       = "ics_1MvSieLkdIwHu7ixUjlsa0iP"
}

output "bundle_card_logo" {
  value = data.stripe_issuing_physical_bundle.standard.features.card_logo
}
