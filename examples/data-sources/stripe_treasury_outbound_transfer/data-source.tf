variable "connected_account" {
  type        = string
  description = "The connected account the object lives on."
}

provider "stripe" {
  alias          = "connected"
  stripe_account = var.connected_account
}

data "stripe_treasury_outbound_transfer" "sweep" {
  provider = stripe.connected
  id       = "obt_1MtYCCLkdIwHu7ixGq6ws9GD"
}

output "outbound_transfer_status" {
  value = data.stripe_treasury_outbound_transfer.sweep.status
}
