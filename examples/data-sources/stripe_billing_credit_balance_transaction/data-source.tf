data "stripe_billing_credit_balance_transaction" "granted" {
  id = "cbtxn_123"
}

output "credit_type" {
  value = data.stripe_billing_credit_balance_transaction.granted.credit.type
}
