data "stripe_payment_record" "in_store" {
  id = "pr_123"
}

output "amount_guaranteed" {
  value = data.stripe_payment_record.in_store.amount_guaranteed.value
}
