data "stripe_billing_meter_event_summary" "october" {
  meter      = "mtr_123"
  customer   = "cus_123"
  start_time = 1790812800 # 2026-10-01T00:00:00Z
  end_time   = 1793491200 # 2026-11-01T00:00:00Z
}

output "api_calls_in_october" {
  value = data.stripe_billing_meter_event_summary.october.aggregated_value
}
