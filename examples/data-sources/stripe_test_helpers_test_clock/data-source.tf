data "stripe_test_helpers_test_clock" "billing" {
  id = stripe_test_helpers_test_clock.billing.id
}

output "clock_status" {
  value = data.stripe_test_helpers_test_clock.billing.status
}
