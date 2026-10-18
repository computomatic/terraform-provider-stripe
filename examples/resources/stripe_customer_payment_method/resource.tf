resource "stripe_customer" "acme" {
  email = "billing@acme.example"
}

# A payment method made elsewhere, such as by Stripe.js or a SetupIntent. A Stripe test
# payment method such as "pm_card_visa" works in a sandbox.
variable "payment_method_id" {
  type = string
}

resource "stripe_customer_payment_method" "acme" {
  customer       = stripe_customer.acme.id
  payment_method = var.payment_method_id

  billing_details = {
    name  = "Acme Billing"
    email = "billing@acme.example"
  }
}
