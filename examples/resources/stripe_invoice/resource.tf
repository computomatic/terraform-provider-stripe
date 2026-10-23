resource "stripe_customer" "acme" {
  email = "billing@acme.example"
}

# A pending item, which the invoice sweeps up when it is created.
resource "stripe_invoiceitem" "setup_fee" {
  customer    = stripe_customer.acme.id
  amount      = 5000
  currency    = "usd"
  description = "One-time setup fee"
}

# Finalized and left open for the customer to pay from the emailed invoice.
resource "stripe_invoice" "setup" {
  customer                       = stripe_customer.acme.id
  currency                       = "usd"
  collection_method              = "send_invoice"
  days_until_due                 = 30
  auto_advance                   = false
  pending_invoice_items_behavior = "include"
  status                         = "open"

  depends_on = [stripe_invoiceitem.setup_fee]
}
