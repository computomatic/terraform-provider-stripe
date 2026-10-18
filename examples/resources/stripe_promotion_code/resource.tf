resource "stripe_coupon" "launch" {
  percent_off = 10
  duration    = "once"
}

resource "stripe_promotion_code" "launch" {
  code            = "LAUNCH10"
  max_redemptions = 100

  promotion = {
    type   = "coupon"
    coupon = stripe_coupon.launch.id
  }

  restrictions = {
    first_time_transaction  = true
    minimum_amount          = 1000
    minimum_amount_currency = "usd"
  }
}
