data "stripe_entitlements_active_entitlement" "premium" {
  id = "ent_123"
}

output "feature_lookup_key" {
  value = data.stripe_entitlements_active_entitlement.premium.lookup_key
}
