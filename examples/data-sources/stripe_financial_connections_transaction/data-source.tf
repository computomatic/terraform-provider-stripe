data "stripe_financial_connections_transaction" "latest" {
  id = "fctxn_1MwVKA2eZvKYlo2CqJ2kN5Ze"
}

output "transaction_amount" {
  value = data.stripe_financial_connections_transaction.latest.amount
}
