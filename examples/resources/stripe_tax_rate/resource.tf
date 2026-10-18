resource "stripe_tax_rate" "vat" {
  display_name = "VAT"
  description  = "UK VAT"
  jurisdiction = "GB"
  country      = "GB"
  percentage   = 20
  inclusive    = false
}
