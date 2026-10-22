data "stripe_quote_computed_upfront_line_item" "first" {
  quote = "qt_123"
  id    = "li_123"
}

output "upfront_total" {
  value = data.stripe_quote_computed_upfront_line_item.first.amount_total
}
