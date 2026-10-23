resource "stripe_payment_intent" "order" {
  amount         = 2000
  currency       = "usd"
  payment_method = "pm_card_visa"
  status         = "succeeded"

  automatic_payment_methods = {
    enabled         = true
    allow_redirects = "never"
  }
}

# Refund part of the payment. Without `amount`, the refund is for what remains.
resource "stripe_refund" "partial" {
  payment_intent = stripe_payment_intent.order.id
  amount         = 500
  reason         = "requested_by_customer"
}
