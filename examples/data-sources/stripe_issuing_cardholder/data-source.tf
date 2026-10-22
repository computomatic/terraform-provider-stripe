variable "connected_account" {
  type        = string
  description = "The connected account the object lives on."
}

provider "stripe" {
  alias          = "connected"
  stripe_account = var.connected_account
}

data "stripe_issuing_cardholder" "jenny" {
  provider = stripe.connected
  id       = "ich_1MsKAB2eZvKYlo2C3eZ2BdvK"
}

output "cardholder_status" {
  value = data.stripe_issuing_cardholder.jenny.status
}
