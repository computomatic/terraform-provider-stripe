variable "connected_account" {
  type        = string
  description = "The connected account the object lives on."
}

provider "stripe" {
  alias          = "connected"
  stripe_account = var.connected_account
}

data "stripe_treasury_credit_reversal" "returned" {
  provider = stripe.connected
  id       = "credrev_1MtYCCLkdIwHu7ixJ0TnOgZr"
}

output "credit_reversal_status" {
  value = data.stripe_treasury_credit_reversal.returned.status
}
