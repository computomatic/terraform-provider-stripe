resource "stripe_customer" "acme" {
  email = "billing@acme.example"
}

resource "stripe_product" "setup" {
  name = "Onboarding"
}

resource "stripe_price" "setup" {
  product     = stripe_product.setup.id
  currency    = "usd"
  unit_amount = 25000
}

resource "stripe_quote" "onboarding" {
  customer          = stripe_customer.acme.id
  description       = "Onboarding package"
  collection_method = "send_invoice"

  invoice_settings = {
    days_until_due = 30
  }

  line_items = [{
    price    = stripe_price.setup.id
    quantity = 1
  }]
}
