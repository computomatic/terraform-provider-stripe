data "stripe_invoice" "monthly" {
  id = "in_123"
}

data "stripe_invoice_line" "first" {
  invoice = data.stripe_invoice.monthly.id
  id      = data.stripe_invoice.monthly.lines.data[0].id
}

output "line_amount" {
  value = data.stripe_invoice_line.first.amount
}
