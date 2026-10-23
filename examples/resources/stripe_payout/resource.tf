# Pay out test funds to the account's default bank account in USD.
resource "stripe_payout" "weekly" {
  amount      = 5000
  currency    = "usd"
  description = "Weekly payout"
  metadata = {
    run = "demo"
  }
}
