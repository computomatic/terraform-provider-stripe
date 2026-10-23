data "stripe_payment_link_line_item" "first" {
  payment_link = "plink_123"
  id           = "li_123"
}

output "item_total" {
  value = data.stripe_payment_link_line_item.first.amount_total
}
