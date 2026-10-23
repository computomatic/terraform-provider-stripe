# Authorizes 20.00 on Stripe's test card. Setting `captured = true` later captures it.
resource "stripe_charge" "deposit" {
  amount      = 2000
  currency    = "usd"
  source      = "tok_visa"
  capture     = false
  description = "Deposit"
}
