data "stripe_account_v2" "seller" {
  id = "acct_1Nv0FGQ9RKHgCVdK"
}

output "seller_card_payments" {
  value = data.stripe_account_v2.seller.configuration.merchant.capabilities.card_payments.status
}
