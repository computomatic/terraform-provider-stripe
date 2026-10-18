data "stripe_climate_product" "removal" {
  id = "climsku_123"
}

output "delivery_year" {
  value = data.stripe_climate_product.removal.delivery_year
}
