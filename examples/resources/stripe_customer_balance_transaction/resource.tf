resource "stripe_customer" "acme" {
  name = "Acme GmbH"
}

# A negative amount credits the customer.
resource "stripe_customer_balance_transaction" "goodwill" {
  customer    = stripe_customer.acme.id
  amount      = -500
  currency    = "usd"
  description = "Goodwill credit"
}
