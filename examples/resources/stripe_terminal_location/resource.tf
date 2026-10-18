resource "stripe_terminal_location" "store" {
  display_name = "Downtown store"

  address = {
    line1       = "354 Oyster Point Blvd"
    city        = "South San Francisco"
    state       = "CA"
    postal_code = "94080"
    country     = "US"
  }
}
