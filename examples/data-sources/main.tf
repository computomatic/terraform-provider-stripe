terraform {
  required_providers {
    stripe = {
      source = "example.com/computomatic/stripe"
    }
  }
}

provider "stripe" {
  # Set the Stripe API key in the STRIPE_API_KEY environment variable
}

data "stripe_balance" "current" {}

data "stripe_product" "example" {
  id = "prod_S6FuCl4cN7DvwU"
}

output "example_product" {
  value = data.stripe_product.example
}

output "balance" {
  value = data.stripe_balance.current
}
