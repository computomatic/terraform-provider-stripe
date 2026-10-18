resource "stripe_price" "addon" {
  product     = stripe_product.plan.id
  currency    = "usd"
  unit_amount = 500

  recurring = {
    interval = "month"
  }
}

# An item added here shows up in the subscription's `items`, so the subscription ignores them.
resource "stripe_subscription" "team" {
  customer          = stripe_customer.acme.id
  collection_method = "send_invoice"
  days_until_due    = 30
  items             = [{ price = stripe_price.monthly.id }]

  lifecycle {
    ignore_changes = [items]
  }
}

resource "stripe_subscription_item" "addon" {
  subscription = stripe_subscription.team.id
  price        = stripe_price.addon.id
  quantity     = 2

  # Skip the prorated invoice line that adding an item mid-period creates.
  proration_behavior = "none"
}
