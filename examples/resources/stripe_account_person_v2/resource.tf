# Stripe's test values for a person who always verifies. In live mode, pass the person's
# identity numbers from sensitive variables.
resource "stripe_account_person_v2" "owner" {
  account_id = stripe_account_v2.seller.id
  given_name = "Owen"
  surname    = "Rosen"
  email      = "owen.rosen@example.com"
  phone      = "0000000000"

  date_of_birth = {
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

  id_numbers = [
    { type = "us_ssn_last_4", value = "0000" },
  ]

  relationship = {
    owner             = true
    percent_ownership = "60"
  }
}
