data "stripe_transfer" "payout_to_seller" {
  id = "tr_123"
}

output "transfer_destination" {
  value = data.stripe_transfer.payout_to_seller.destination
}
