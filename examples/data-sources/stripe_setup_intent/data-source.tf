data "stripe_setup_intent" "card" {
  id = stripe_setup_intent.card.id
}
