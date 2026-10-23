data "stripe_core_account" "seller" {
  id = "acct_1Nv0FGQ9RKHgCVdK"
}

output "seller_card_payments" {
  value = data.stripe_core_account.seller.configuration.merchant.capabilities.card_payments.status
}
