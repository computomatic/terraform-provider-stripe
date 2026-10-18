resource "stripe_product" "gold" {
  name = "Gold plan"
}

resource "stripe_price" "trial" {
  product     = stripe_product.gold.id
  currency    = "usd"
  unit_amount = 100

  recurring = {
    interval = "month"
  }
}

resource "stripe_price" "full" {
  product     = stripe_product.gold.id
  currency    = "usd"
  unit_amount = 1000

  recurring = {
    interval = "month"
  }
}

resource "stripe_product_catalog_trial_offer" "first_month" {
  price    = stripe_price.trial.id
  nickname = "First month"

  duration = {
    type     = "relative"
    relative = { iterations = 1 }
  }

  end_behavior = {
    transition = { price = stripe_price.full.id }
  }
}
