data "stripe_radar_early_fraud_warning" "warning" {
  id = "issfr_123"
}

output "fraud_type" {
  value = data.stripe_radar_early_fraud_warning.warning.fraud_type
}
