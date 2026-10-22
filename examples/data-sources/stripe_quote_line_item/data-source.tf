data "stripe_quote_line_item" "first" {
  quote = "qt_123"
  id    = "li_123"
}

output "item_total" {
  value = data.stripe_quote_line_item.first.amount_total
}
