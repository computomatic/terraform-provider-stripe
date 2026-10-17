resource "stripe_customer" "acme" {
  name  = "Acme Inc."
  email = "billing@acme.example"

  metadata = {
    account = "acme"
  }
}
