data "stripe_checkout_session_line_item" "first" {
  checkout_session = "cs_test_123"
  id               = "li_123"
}

output "item_total" {
  value = data.stripe_checkout_session_line_item.first.amount_total
}
