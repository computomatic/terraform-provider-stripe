variable "connected_account" {
  type        = string
  description = "The connected account the object lives on."
}

provider "stripe" {
  alias          = "connected"
  stripe_account = var.connected_account
}

data "stripe_treasury_outbound_payment" "supplier" {
  provider = stripe.connected
  id       = "obp_1MtYCCLkdIwHu7ixgnB6x2u9"
}

output "outbound_payment_status" {
  value = data.stripe_treasury_outbound_payment.supplier.status
}
