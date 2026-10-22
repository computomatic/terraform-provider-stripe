data "stripe_financial_connections_account" "checking" {
  id = "fca_1MwVK82eZvKYlo2Cjw8FMxXf"
}

output "institution_name" {
  value = data.stripe_financial_connections_account.checking.institution_name
}
