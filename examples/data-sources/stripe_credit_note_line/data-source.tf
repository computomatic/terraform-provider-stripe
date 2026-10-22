data "stripe_credit_note_line" "first" {
  credit_note = "cn_123"
  id          = "cnli_123"
}

output "line_amount" {
  value = data.stripe_credit_note_line.first.amount
}
