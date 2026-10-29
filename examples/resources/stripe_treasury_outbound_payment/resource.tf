variable "treasury_account" {
  type        = string
  description = "A connected account with the treasury capability active."
}

variable "financial_account" {
  type        = string
  description = "A financial account of the connected account."
}

# Treasury runs on a connected account, so a second provider configuration acts on it.
provider "stripe" {
  alias          = "treasury"
  stripe_account = var.treasury_account
}

resource "stripe_treasury_outbound_payment" "supplier" {
  provider          = stripe.treasury
  financial_account = var.financial_account
  amount            = 2500
  currency          = "usd"
  description       = "Invoice 1042"

  destination_payment_method_data = {
    type = "us_bank_account"
    us_bank_account = {
      routing_number      = "110000000"
      account_number      = "000123456789"
      account_holder_type = "company"
    }
    billing_details = {
      name = "Supplier Ltd"
    }
  }
}

# Stripe posts a new payment within about a second.
output "outbound_payment_status" {
  value = stripe_treasury_outbound_payment.supplier.status
}
