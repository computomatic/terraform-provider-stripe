data "stripe_customer_balance_transaction" "goodwill" {
  customer = stripe_customer.acme.id
  id       = stripe_customer_balance_transaction.goodwill.id
}
