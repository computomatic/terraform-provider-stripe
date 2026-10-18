resource "stripe_core_event_destination" "billing" {
  name           = "billing events"
  type           = "webhook_endpoint"
  event_payload  = "thin"
  enabled_events = ["v1.billing.meter.error_report_triggered"]

  webhook_endpoint = {
    url = "https://example.com/stripe/events"
  }
}
