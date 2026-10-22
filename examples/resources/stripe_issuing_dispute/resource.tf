variable "issuing_account" {
  type        = string
  description = "A connected account with the card_issuing capability active."
}

variable "transaction" {
  type        = string
  description = "The Issuing transaction the cardholder disputes."
}

# Issuing runs on a connected account, so a second provider configuration acts on it.
provider "stripe" {
  alias          = "issuing"
  stripe_account = var.issuing_account
}

resource "stripe_issuing_dispute" "unrecognised" {
  provider    = stripe.issuing
  transaction = var.transaction

  evidence = {
    reason = "fraudulent"
    fraudulent = {
      explanation = "The cardholder did not make this purchase."
    }
  }

  # Submit once the evidence is complete. Stripe refuses changes to the evidence afterwards.
  status = "submitted"
}
