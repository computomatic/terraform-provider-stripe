variable "issuing_account" {
  type        = string
  description = "A connected account with the card_issuing and treasury capabilities active."
}

variable "cardholder" {
  type        = string
  description = "The cardholder the card is issued to."
}

# Issuing runs on a connected account, so a second provider configuration acts on it.
provider "stripe" {
  alias          = "issuing"
  stripe_account = var.issuing_account
}

# Cards on a connected account draw on one of its Treasury financial accounts.
resource "stripe_treasury_financial_account" "cards" {
  provider             = stripe.issuing
  supported_currencies = ["usd"]
  features = {
    card_issuing = { requested = true }
  }
}

resource "stripe_issuing_card" "travel" {
  provider          = stripe.issuing
  cardholder        = var.cardholder
  currency          = "usd"
  type              = "virtual"
  financial_account = stripe_treasury_financial_account.cards.id

  # A new card is inactive. Set status = "active" once the cardholder's review has finished.

  spending_controls = {
    spending_limits = [{ amount = 20000, interval = "daily" }]
  }
}
