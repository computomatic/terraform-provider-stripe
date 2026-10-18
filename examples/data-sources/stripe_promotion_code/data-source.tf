data "stripe_promotion_code" "by_id" {
  id = stripe_promotion_code.launch.id
}

data "stripe_promotion_code" "by_code" {
  code = "LAUNCH10"
}
