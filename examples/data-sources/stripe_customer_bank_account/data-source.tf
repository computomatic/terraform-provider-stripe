resource "stripe_customer" "acme" {
  name = "Acme GmbH"
}

data "stripe_customer_bank_account" "ach" {
  customer = stripe_customer.acme.id
  id       = "ba_123"
}

output "bank_account_status" {
  value = data.stripe_customer_bank_account.ach.status
}
