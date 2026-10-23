# A merchant whose platform collects its fees and covers its losses, without a Stripe
# dashboard.
resource "stripe_core_account" "seller" {
  display_name  = "Acme Supplies"
  contact_email = "payments@acme.example"
  dashboard     = "none"

  identity = {
    country     = "US"
    entity_type = "company"
    business_details = {
      registered_name = "Acme Supplies Inc."
    }
  }

  configuration = {
    merchant = {
      capabilities = {
        card_payments = { requested = true }
      }
    }
  }

  defaults = {
    currency = "usd"
    responsibilities = {
      fees_collector   = "application"
      losses_collector = "application"
    }
  }

  metadata = {
    team = "marketplace"
  }
}
