resource "stripe_transfer" "payout_share" {
  amount      = 1500
  currency    = "usd"
  destination = "acct_123"
}

# Move part of the transfer back to the platform. Without `amount`, the rest is reversed.
resource "stripe_transfer_reversal" "returned_item" {
  transfer = stripe_transfer.payout_share.id
  amount   = 500
  metadata = { reason = "returned item" }
}
