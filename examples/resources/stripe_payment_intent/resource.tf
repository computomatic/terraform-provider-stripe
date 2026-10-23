resource "stripe_customer" "acme" {
  email = "billing@acme.example"
}

# A succeeded test payment: the intent is confirmed with Stripe's test card in the same apply.
resource "stripe_payment_intent" "order" {
  amount         = 2000
  currency       = "usd"
  customer       = stripe_customer.acme.id
  payment_method = "pm_card_visa"
  status         = "succeeded"

  automatic_payment_methods = {
    enabled         = true
    allow_redirects = "never"
  }
}
