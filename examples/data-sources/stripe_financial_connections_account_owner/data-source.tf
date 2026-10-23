data "stripe_financial_connections_account_owner" "holder" {
  account = "fca_1MwVK82eZvKYlo2Cjw8FMxXf"
  id      = "fcaown_1MwVK92eZvKYlo2CaY4DmLpV"
}

output "owner_name" {
  value = data.stripe_financial_connections_account_owner.holder.name
}
