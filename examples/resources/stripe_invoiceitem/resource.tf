resource "stripe_customer" "acme" {
  email = "billing@acme.example"
}

# Added to the customer's next invoice.
resource "stripe_invoiceitem" "setup_fee" {
  customer    = stripe_customer.acme.id
  amount      = 5000
  currency    = "usd"
  description = "One-time setup fee"
}
