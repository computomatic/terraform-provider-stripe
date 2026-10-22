data "stripe_tax_transaction_line_item" "first" {
  transaction = "tax_123"
  id          = "tax_li_123"
}

output "line_tax" {
  value = data.stripe_tax_transaction_line_item.first.amount_tax
}
