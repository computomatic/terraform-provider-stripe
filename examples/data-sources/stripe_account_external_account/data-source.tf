data "stripe_account_external_account" "payouts" {
  account = stripe_account.seller.id
  id      = stripe_account_external_account.payouts.id
}
