resource "stripe_account" "seller" {
  type    = "custom"
  country = "US"

  # A custom account must request at least one capability.
  capabilities = {
    transfers = { requested = true }
  }
}

# Stripe's test values for a person who always verifies. In live mode, pass the person's
# identity numbers from sensitive variables.
resource "stripe_account_person" "representative" {
  account    = stripe_account.seller.id
  first_name = "Jenny"
  last_name  = "Rosen"
  email      = "jenny.rosen@example.com"
  phone      = "0000000000"
  id_number  = "000000000"
  ssn_last_4 = "0000"

  dob = {
    day   = 1
    month = 1
    year  = 1901
  }

  address = {
    line1       = "address_full_match"
    city        = "South San Francisco"
    state       = "CA"
    postal_code = "94080"
    country     = "US"
  }

  relationship = {
    representative = true
    executive      = true
    title          = "CEO"
  }
}
