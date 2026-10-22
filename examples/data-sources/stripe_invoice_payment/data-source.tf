data "stripe_invoice_payment" "paid" {
  id = "inpay_123"
}

output "amount_paid" {
  value = data.stripe_invoice_payment.paid.amount_paid
}
