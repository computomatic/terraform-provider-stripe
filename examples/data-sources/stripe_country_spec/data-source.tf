data "stripe_country_spec" "us" {
  id = "US"
}

output "us_default_currency" {
  value = data.stripe_country_spec.us.default_currency
}
