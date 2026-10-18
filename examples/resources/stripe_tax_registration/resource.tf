resource "stripe_tax_registration" "gb" {
  country     = "GB"
  active_from = "now"

  country_options = {
    gb = {
      type = "standard"
    }
  }
}
