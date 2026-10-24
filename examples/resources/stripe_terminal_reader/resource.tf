resource "stripe_terminal_location" "store" {
  display_name = "Main Street store"

  address = {
    line1       = "354 Oyster Point Blvd"
    city        = "South San Francisco"
    state       = "CA"
    postal_code = "94080"
    country     = "US"
  }
}

# Enter the code the reader shows when you register it. In test mode, `simulated-wpe`
# registers a simulated reader.
resource "stripe_terminal_reader" "counter" {
  registration_code = "simulated-wpe"
  location          = stripe_terminal_location.store.id
  label             = "Front counter"

  metadata = {
    lane = "1"
  }
}
