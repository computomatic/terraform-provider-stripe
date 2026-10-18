resource "stripe_billing_portal_configuration" "default" {
  name               = "Customer portal"
  default_return_url = "https://example.com/account"

  business_profile = {
    headline = "Manage your subscription"
  }

  features = {
    invoice_history = { enabled = true }

    customer_update = {
      enabled         = true
      allowed_updates = ["email", "address"]
    }

    subscription_cancel = {
      enabled = true
      mode    = "at_period_end"
    }
  }
}
