data "stripe_account_person" "representative" {
  account = stripe_account.seller.id
  id      = stripe_account_person.representative.id
}

output "representative_verification" {
  value = data.stripe_account_person.representative.verification.status
}
