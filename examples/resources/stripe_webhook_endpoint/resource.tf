resource "stripe_webhook_endpoint" "events" {
  url            = "https://example.com/stripe/webhook"
  enabled_events = ["invoice.paid", "customer.subscription.deleted"]
}

# The signing secret is sensitive and set once, when the endpoint is created.
output "webhook_signing_secret" {
  value     = stripe_webhook_endpoint.events.secret
  sensitive = true
}
