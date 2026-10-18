resource "stripe_customer" "acme" {
  email = "billing@acme.example"
}

resource "stripe_product" "plan" {
  name = "Team plan"
}

resource "stripe_price" "monthly" {
  product     = stripe_product.plan.id
  currency    = "usd"
  unit_amount = 2000

  recurring = {
    interval = "month"
  }
}

# Invoiced by email, so the customer needs no payment method.
resource "stripe_subscription" "team" {
  customer          = stripe_customer.acme.id
  collection_method = "send_invoice"
  days_until_due    = 30

  items = [{
    price    = stripe_price.monthly.id
    quantity = 5
  }]
}
