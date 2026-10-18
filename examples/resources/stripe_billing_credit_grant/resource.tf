resource "stripe_customer" "acme" {
  email = "billing@acme.example"
}

resource "stripe_billing_credit_grant" "welcome" {
  name     = "Welcome credit"
  customer = stripe_customer.acme.id
  category = "promotional"

  amount = {
    type = "monetary"
    monetary = {
      currency = "usd"
      value    = 2500
    }
  }

  applicability_config = {
    scope = {
      price_type = "metered"
    }
  }
}
