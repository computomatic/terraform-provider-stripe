resource "stripe_product" "gold" {
  name = "Gold plan"
}

resource "stripe_entitlements_feature" "advanced_search" {
  lookup_key = "advanced_search"
  name       = "Advanced search"
}

resource "stripe_product_feature" "gold_search" {
  product             = stripe_product.gold.id
  entitlement_feature = stripe_entitlements_feature.advanced_search.id
}

# Import with the product ID and the product feature ID:
#   terraform import stripe_product_feature.gold_search prod_123/prodft_456
