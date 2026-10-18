resource "stripe_shipping_rate" "ground" {
  display_name = "Ground shipping"
  tax_behavior = "exclusive"

  fixed_amount = {
    amount   = 500
    currency = "usd"
  }

  delivery_estimate = {
    minimum = { unit = "business_day", value = 3 }
    maximum = { unit = "business_day", value = 5 }
  }
}
