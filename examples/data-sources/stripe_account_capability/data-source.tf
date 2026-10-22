data "stripe_account_capability" "card_payments" {
  account = stripe_account.seller.id
  id      = "card_payments"
}

output "card_payments_status" {
  value = data.stripe_account_capability.card_payments.status
}
