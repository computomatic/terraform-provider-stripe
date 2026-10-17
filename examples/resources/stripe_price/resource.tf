resource "stripe_product" "gold" {
  name = "Gold plan"
}

resource "stripe_price" "monthly" {
  product     = stripe_product.gold.id
  currency    = "usd"
  unit_amount = 2000
  lookup_key  = "gold_monthly"

  recurring = {
    interval = "month"
  }
}
