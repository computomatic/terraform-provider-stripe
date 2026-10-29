data "stripe_account_person_v2" "owner" {
  account_id = stripe_account_v2.seller.id
  id         = stripe_account_person_v2.owner.id
}

output "owner_since" {
  value = data.stripe_account_person_v2.owner.created
}
