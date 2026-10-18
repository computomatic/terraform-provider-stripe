resource "stripe_payment_method_configuration" "checkout" {
  name = "Checkout"

  card = {
    display_preference = {
      preference = "on"
    }
  }

  link = {
    display_preference = {
      preference = "off"
    }
  }
}
