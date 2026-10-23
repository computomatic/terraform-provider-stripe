data "stripe_core_account_person" "owner" {
  account_id = stripe_core_account.seller.id
  id         = stripe_core_account_person.owner.id
}

output "owner_since" {
  value = data.stripe_core_account_person.owner.created
}
