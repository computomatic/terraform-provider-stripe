data "stripe_climate_supplier" "supplier" {
  id = "climsup_123"
}

output "removal_pathway" {
  value = data.stripe_climate_supplier.supplier.removal_pathway
}
