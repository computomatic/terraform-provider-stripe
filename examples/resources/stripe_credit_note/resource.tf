resource "stripe_customer" "acme" {
  email = "billing@acme.example"
}

resource "stripe_invoiceitem" "setup_fee" {
  customer = stripe_customer.acme.id
  amount   = 5000
  currency = "usd"
}

resource "stripe_invoice" "setup" {
  customer                       = stripe_customer.acme.id
  currency                       = "usd"
  collection_method              = "send_invoice"
  days_until_due                 = 30
  pending_invoice_items_behavior = "include"
  status                         = "open"

  depends_on = [stripe_invoiceitem.setup_fee]
}

# Takes 10.00 off the open invoice.
resource "stripe_credit_note" "goodwill" {
  invoice = stripe_invoice.setup.id
  reason  = "order_change"
  memo    = "Goodwill credit"

  lines = [{
    type        = "custom_line_item"
    description = "Goodwill credit"
    quantity    = 1
    unit_amount = 1000
  }]
}
