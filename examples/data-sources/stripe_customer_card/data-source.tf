resource "stripe_customer" "acme" {
  name = "Acme GmbH"
}

data "stripe_customer_card" "saved" {
  customer = stripe_customer.acme.id
  id       = "card_123"
}

output "card_last4" {
  value = data.stripe_customer_card.saved.last4
}
