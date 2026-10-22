variable "connected_account" {
  type        = string
  description = "The connected account the object lives on."
}

provider "stripe" {
  alias          = "connected"
  stripe_account = var.connected_account
}

data "stripe_treasury_received_credit" "deposit" {
  provider = stripe.connected
  id       = "rc_1MtYCCLkdIwHu7ixxw5H2DQd"
}

output "received_credit_status" {
  value = data.stripe_treasury_received_credit.deposit.status
}
