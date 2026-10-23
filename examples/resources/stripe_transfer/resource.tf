# Move test funds from the platform's available balance to a connected account.
resource "stripe_transfer" "payout_share" {
  amount         = 1500
  currency       = "usd"
  destination    = "acct_123"
  transfer_group = "order-1"
  description    = "Seller share of order 1"
}
