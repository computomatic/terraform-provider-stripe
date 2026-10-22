variable "issuing_account" {
  type        = string
  description = "A connected account with the card_issuing capability active."
}

variable "card_logo" {
  type        = string
  description = "A 1000 by 200 pixel black and white PNG uploaded to the connected account with the purpose issuing_logo."
}

# Issuing runs on a connected account, so a second provider configuration acts on it.
provider "stripe" {
  alias          = "issuing"
  stripe_account = var.issuing_account
}

resource "stripe_issuing_personalization_design" "standard" {
  provider        = stripe.issuing
  physical_bundle = "ics_1MvSieLkdIwHu7ixUjlsa0iP"
  card_logo       = var.card_logo
  name            = "Standard card"
  lookup_key      = "standard_card"

  carrier_text = {
    header_title = "Welcome"
    header_body  = "Your new card is enclosed."
    footer_title = "Questions"
    footer_body  = "Contact support."
  }
}

# A new design is in review until Stripe makes it active or rejects it.
output "design_status" {
  value = stripe_issuing_personalization_design.standard.status
}
