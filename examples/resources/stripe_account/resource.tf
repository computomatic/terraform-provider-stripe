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

# A custom account onboarded for Issuing and Treasury. The values are the test values
# Stripe verifies at once in a sandbox. The onboarding arguments are sent when the account
# is created, and a plan that changes one of them later fails.
resource "stripe_account" "card_programme" {
  type          = "custom"
  country       = "US"
  business_type = "individual"
  email         = "jenny.rosen@example.com"

  capabilities = {
    card_issuing = { requested = true }
    transfers    = { requested = true }
    treasury     = { requested = true }
  }

  business_profile = {
    name                   = "Rosen Software"
    mcc                    = "5734"
    url                    = "https://accessible.stripe.com"
    product_description    = "Accounting software for small businesses"
    estimated_worker_count = 1
    annual_revenue = {
      amount          = 10000000
      currency        = "usd"
      fiscal_year_end = "2025-12-31"
    }
  }

  individual = {
    first_name = "Jenny"
    last_name  = "Rosen"
    email      = "jenny.rosen@example.com"
    phone      = "0000000000"
    # Sensitive: shown as (sensitive value) in plans.
    id_number  = "000000000"
    ssn_last_4 = "0000"
    dob        = { day = 1, month = 1, year = 1901 }
    address = {
      line1       = "address_full_match"
      city        = "South San Francisco"
      state       = "CA"
      postal_code = "94080"
    }
    # The ID of an identity document file uploaded with the purpose identity_document.
    verification = {
      document = { front = "file_identity_document_success" }
    }
  }

  # A bank account token, which becomes the account's default bank account for payouts.
  external_account = "btok_us_verified"

  # The Stripe Services Agreement, and the Issuing and Treasury terms, each accepted by
  # the account holder at this time from this IP address.
  tos_acceptance = { date = 1609459200, ip = "8.8.8.8" }
  settings = {
    card_issuing = { tos_acceptance = { date = 1609459200, ip = "8.8.8.8" } }
    treasury     = { tos_acceptance = { date = 1609459200, ip = "8.8.8.8" } }
  }
}
