data "stripe_product_feature" "gold_search" {
  product = stripe_product.gold.id
  id      = stripe_product_feature.gold_search.id
}
