resource "stripe_customer" "acme" {
  name = "Acme GmbH"
}

data "stripe_customer_cash_balance_transaction" "funding" {
  customer = stripe_customer.acme.id
  id       = "ccsbtxn_123"
}
