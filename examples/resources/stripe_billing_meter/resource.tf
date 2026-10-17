resource "stripe_billing_meter" "api_calls" {
  display_name = "API calls"
  event_name   = "api_call"

  default_aggregation = {
    formula = "sum"
  }

  customer_mapping = {
    type              = "by_id"
    event_payload_key = "stripe_customer_id"
  }

  value_settings = {
    event_payload_key = "value"
  }
}
