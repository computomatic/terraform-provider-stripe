# Give part of an application fee back to the connected account. Without `amount`, the rest
# of the fee is refunded.
resource "stripe_application_fee_refund" "goodwill" {
  fee      = "fee_123"
  amount   = 50
  metadata = { reason = "goodwill" }
}
