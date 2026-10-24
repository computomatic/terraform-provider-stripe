resource "stripe_account" "seller" {
  type    = "custom"
  country = "US"

  # A custom account must request at least one capability.
  capabilities = {
    transfers = { requested = true }
  }
}

# Stripe's test bank account for the United States. In live mode, pass the seller's own
# routing and account numbers from a sensitive variable.
resource "stripe_account_external_account" "payouts" {
  account        = stripe_account.seller.id
  country        = "US"
  currency       = "usd"
  routing_number = "110000000"
  account_number = "000123456789"

  account_holder_name = "Acme Supplies"
  account_holder_type = "company"
}
