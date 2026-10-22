data "stripe_tax_transaction" "order" {
  id = "tax_123"
}

output "order_reference" {
  value = data.stripe_tax_transaction.order.reference
}
