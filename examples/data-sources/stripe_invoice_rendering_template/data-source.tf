data "stripe_invoice_rendering_template" "branded" {
  id = "inrtem_123"
}

output "template_version" {
  value = data.stripe_invoice_rendering_template.branded.version
}
