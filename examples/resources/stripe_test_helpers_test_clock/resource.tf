resource "stripe_test_helpers_test_clock" "billing" {
  name        = "Renewal test"
  frozen_time = 1790000000
}

resource "stripe_customer" "acme" {
  name       = "Acme GmbH"
  test_clock = stripe_test_helpers_test_clock.billing.id
}
