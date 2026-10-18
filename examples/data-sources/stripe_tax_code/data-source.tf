data "stripe_tax_code" "general_services" {
  id = "txcd_20030000"
}

resource "stripe_product" "consulting" {
  name     = "Consulting"
  tax_code = data.stripe_tax_code.general_services.id
}
