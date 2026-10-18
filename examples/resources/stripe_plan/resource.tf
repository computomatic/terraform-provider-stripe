resource "stripe_product" "gold" {
  name = "Gold plan"
}

resource "stripe_plan" "monthly" {
  product  = stripe_product.gold.id
  currency = "usd"
  interval = "month"
  amount   = 2000
  nickname = "Monthly"
}
