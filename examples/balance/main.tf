terraform {
  required_providers {
    stripe = {
      source = "github.com/computomatic/stripe"
    }
  }
}

provider "stripe" {
  # Set the Stripe API key in the STRIPE_API_KEY environment variable
}

data "stripe_balance" "current" {}

output "balance" {
  value = data.stripe_balance.current
}
