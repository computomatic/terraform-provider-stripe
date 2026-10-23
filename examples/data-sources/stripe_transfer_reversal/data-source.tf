data "stripe_transfer_reversal" "refund_to_platform" {
  transfer = "tr_123"
  id       = "trr_123"
}

output "reversed_amount" {
  value = data.stripe_transfer_reversal.refund_to_platform.amount
}
