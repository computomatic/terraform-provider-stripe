data "stripe_payment_method_configuration" "checkout" {
  id = stripe_payment_method_configuration.checkout.id
}
