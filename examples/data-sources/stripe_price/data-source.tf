data "stripe_price" "by_id" {
  id = stripe_price.monthly.id
}

data "stripe_price" "by_lookup_key" {
  lookup_key = "gold_monthly"
}
