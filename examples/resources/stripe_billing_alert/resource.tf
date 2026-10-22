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

resource "stripe_billing_alert" "heavy_usage" {
  alert_type = "usage_threshold"
  title      = "API calls above 1000"

  usage_threshold = {
    gte        = 1000
    meter      = stripe_billing_meter.api_calls.id
    recurrence = "one_time"
  }
}
