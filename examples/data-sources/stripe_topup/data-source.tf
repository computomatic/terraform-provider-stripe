data "stripe_topup" "funding" {
  id = "tu_123"
}

output "topup_status" {
  value = data.stripe_topup.funding.status
}
