data "stripe_payment_method_domain" "checkout" {
  id = stripe_payment_method_domain.checkout.id
}
