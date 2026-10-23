data "stripe_application_fee" "order_fee" {
  id = "fee_123"
}

output "fee_amount" {
  value = data.stripe_application_fee.order_fee.amount
}
