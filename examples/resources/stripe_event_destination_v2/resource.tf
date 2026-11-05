resource "stripe_event_destination_v2" "billing" {
  name           = "billing events"
  type           = "webhook_endpoint"
  event_payload  = "thin"
  enabled_events = ["v1.billing.meter.error_report_triggered"]

  webhook_endpoint = {
    url = "https://example.com/stripe/events"
  }
}

# The signing secret is sensitive and set once, when the destination is created.
output "billing_signing_secret" {
  value     = stripe_event_destination_v2.billing.webhook_endpoint.signing_secret
  sensitive = true
}
