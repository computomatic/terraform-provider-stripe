variable "connected_account" {
  type        = string
  description = "The connected account the object lives on."
}

provider "stripe" {
  alias          = "connected"
  stripe_account = var.connected_account
}

data "stripe_treasury_received_debit" "pull" {
  provider = stripe.connected
  id       = "rd_1MtYCCLkdIwHu7ixP2fzRHwe"
}

output "received_debit_status" {
  value = data.stripe_treasury_received_debit.pull.status
}
