data "stripe_payout" "latest" {
  id = "po_123"
}

output "payout_status" {
  value = data.stripe_payout.latest.status
}
