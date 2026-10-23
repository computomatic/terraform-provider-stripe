# Add test funds to the balance from the account's bank account.
resource "stripe_topup" "funds" {
  amount      = 100000
  currency    = "usd"
  description = "Test funds"
}
