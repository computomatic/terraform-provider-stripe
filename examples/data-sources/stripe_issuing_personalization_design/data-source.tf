variable "connected_account" {
  type        = string
  description = "The connected account the object lives on."
}

provider "stripe" {
  alias          = "connected"
  stripe_account = var.connected_account
}

data "stripe_issuing_personalization_design" "standard" {
  provider   = stripe.connected
  lookup_key = "standard_card"
}

output "design_status" {
  value = data.stripe_issuing_personalization_design.standard.status
}
