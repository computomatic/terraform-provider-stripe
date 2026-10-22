variable "connected_account" {
  type        = string
  description = "The connected account the object lives on."
}

provider "stripe" {
  alias          = "connected"
  stripe_account = var.connected_account
}

data "stripe_issuing_dispute" "unrecognised" {
  provider = stripe.connected
  id       = "idp_1MvSieLkdIwHu7ixOeFkD4Dx"
}

output "dispute_status" {
  value = data.stripe_issuing_dispute.unrecognised.status
}
