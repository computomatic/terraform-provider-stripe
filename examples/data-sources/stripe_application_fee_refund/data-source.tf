data "stripe_application_fee_refund" "goodwill" {
  fee = "fee_123"
  id  = "fr_123"
}

output "refunded_amount" {
  value = data.stripe_application_fee_refund.goodwill.amount
}
