data "stripe_setup_intent" "sepa" {
  id = "seti_123"
}

data "stripe_mandate" "sepa" {
  id = data.stripe_setup_intent.sepa.mandate
}
