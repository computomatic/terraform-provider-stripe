variable "issuing_account" {
  type        = string
  description = "A connected account with the card_issuing capability active."
}

# Issuing runs on a connected account, so a second provider configuration acts on it.
provider "stripe" {
  alias          = "issuing"
  stripe_account = var.issuing_account
}

resource "stripe_issuing_cardholder" "jenny" {
  provider     = stripe.issuing
  type         = "individual"
  name         = "Jenny Rosen"
  email        = "jenny.rosen@example.com"
  phone_number = "+18008675309"

  individual = {
    first_name = "Jenny"
    last_name  = "Rosen"
    dob        = { day = 1, month = 1, year = 1990 }
    card_issuing = {
      user_terms_acceptance = { date = 1609459200, ip = "8.8.8.8" }
    }
  }

  billing = {
    address = {
      line1       = "354 Oyster Point Blvd"
      city        = "South San Francisco"
      state       = "CA"
      postal_code = "94080"
      country     = "US"
    }
  }

  spending_controls = {
    spending_limits = [{ amount = 50000, interval = "monthly" }]
  }
}
