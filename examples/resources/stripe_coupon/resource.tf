resource "stripe_coupon" "launch" {
  name        = "Launch offer"
  percent_off = 25
  duration    = "repeating"

  duration_in_months = 3
}
