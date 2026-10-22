data "stripe_core_event" "ping" {
  id = "evt_123"
}

output "event_type" {
  value = data.stripe_core_event.ping.type
}
