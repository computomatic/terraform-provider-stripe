data "stripe_event_v2" "ping" {
  id = "evt_123"
}

output "event_type" {
  value = data.stripe_event_v2.ping.type
}
