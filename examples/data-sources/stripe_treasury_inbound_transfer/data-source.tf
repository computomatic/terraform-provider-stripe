variable "connected_account" {
  type        = string
  description = "The connected account the object lives on."
}

provider "stripe" {
  alias          = "connected"
  stripe_account = var.connected_account
}

data "stripe_treasury_inbound_transfer" "top_up" {
  provider = stripe.connected
  id       = "ibt_1MtYCCLkdIwHu7ixLbF2Xr3h"
}

output "inbound_transfer_status" {
  value = data.stripe_treasury_inbound_transfer.top_up.status
}
