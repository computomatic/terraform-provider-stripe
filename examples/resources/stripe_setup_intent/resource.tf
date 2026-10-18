resource "stripe_customer" "acme" {
  email = "billing@acme.example"
}

# Hand the client secret to your front end, which collects and confirms the payment method.
resource "stripe_setup_intent" "card" {
  customer = stripe_customer.acme.id
  usage    = "off_session"

  automatic_payment_methods = {
    enabled         = true
    allow_redirects = "never"
  }
}

output "client_secret" {
  value     = stripe_setup_intent.card.client_secret
  sensitive = true
}
