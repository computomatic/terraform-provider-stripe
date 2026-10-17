resource "stripe_product" "gold" {
  name        = "Gold plan"
  description = "Everything in Silver, plus priority support."

  metadata = {
    tier = "gold"
  }
}
