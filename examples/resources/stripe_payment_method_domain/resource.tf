resource "stripe_payment_method_domain" "checkout" {
  domain_name = "pay.example.com"
}
