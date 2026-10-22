data "stripe_tax_location" "venue" {
  id = "taxloc_123"
}

output "venue_city" {
  value = data.stripe_tax_location.venue.address.city
}
