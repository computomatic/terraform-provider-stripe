resource "stripe_customer" "acme" {
  email = "billing@acme.example"
}

resource "stripe_product" "plan" {
  name = "Team plan"
}

resource "stripe_price" "intro" {
  product     = stripe_product.plan.id
  currency    = "usd"
  unit_amount = 1000

  recurring = {
    interval = "month"
  }
}

resource "stripe_price" "standard" {
  product     = stripe_product.plan.id
  currency    = "usd"
  unit_amount = 2000

  recurring = {
    interval = "month"
  }
}

# Three months at the introductory price, then the standard price until cancelled.
resource "stripe_subscription_schedule" "team" {
  customer     = stripe_customer.acme.id
  start_date   = "now"
  end_behavior = "release"

  default_settings = {
    collection_method = "send_invoice"
    invoice_settings = {
      days_until_due = 30
    }
  }

  phases = [
    {
      items = [{ price = stripe_price.intro.id }]
      duration = {
        interval       = "month"
        interval_count = 3
      }
    },
    {
      items = [{ price = stripe_price.standard.id }]
    },
  ]
}
