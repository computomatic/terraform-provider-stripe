resource "stripe_customer" "acme" {
  name = "Acme GmbH"
}

resource "stripe_tax_id" "acme_vat" {
  type  = "eu_vat"
  value = "DE123456789"

  owner = {
    type     = "customer"
    customer = stripe_customer.acme.id
  }
}
