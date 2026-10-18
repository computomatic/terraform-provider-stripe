data "stripe_payment_intent" "order" {
  id = "pi_123"
}

data "stripe_charge" "order" {
  id = data.stripe_payment_intent.order.latest_charge
}
