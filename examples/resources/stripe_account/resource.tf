# A custom account in the United States.
resource "stripe_account" "seller" {
  type    = "custom"
  country = "US"
  email   = "seller@example.com"

  business_profile = {
    name = "Acme Supplies"
  }

  # The capabilities the account requests when it is created.
  capabilities = {
    card_payments = { requested = true }
    transfers     = { requested = true }
  }

  metadata = {
    seller = "acme"
  }
}

# An account created from controller properties instead of a type.
resource "stripe_account" "marketplace_seller" {
  country = "US"

  controller = {
    fees                   = { payer = "application" }
    losses                 = { payments = "application" }
    requirement_collection = "stripe"
    stripe_dashboard       = { type = "express" }
  }
}
