data "stripe_payment_intent_amount_details_line_item" "widget" {
  payment_intent = "pi_123"
  id             = "uli_123"
}

output "widget_unit_cost" {
  value = data.stripe_payment_intent_amount_details_line_item.widget.unit_cost
}
