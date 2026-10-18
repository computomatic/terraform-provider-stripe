data "stripe_entitlements_feature" "by_id" {
  id = stripe_entitlements_feature.advanced_search.id
}

data "stripe_entitlements_feature" "by_lookup_key" {
  lookup_key = "advanced_search"
}
