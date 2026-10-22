variable "treasury_account" {
  type        = string
  description = "A connected account with the treasury capability active."
}

# Treasury runs on a connected account, so a second provider configuration acts on it.
provider "stripe" {
  alias          = "treasury"
  stripe_account = var.treasury_account
}

resource "stripe_treasury_financial_account" "operating" {
  provider             = stripe.treasury
  supported_currencies = ["usd"]
  features = {
    card_issuing = { requested = true }
  }
  metadata = {
    purpose = "card spending"
  }
}

output "active_features" {
  value = stripe_treasury_financial_account.operating.active_features
}
