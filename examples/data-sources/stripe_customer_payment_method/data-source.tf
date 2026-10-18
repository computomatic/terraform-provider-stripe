data "stripe_customer_payment_method" "acme" {
  customer = stripe_customer.acme.id
  id       = stripe_customer_payment_method.acme.id
}
