data "stripe_climate_order" "offset" {
  id = "climorder_123"
}

output "order_status" {
  value = data.stripe_climate_order.offset.status
}
