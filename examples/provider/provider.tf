terraform {
  required_providers {
    stripe = {
      source = "computomatic/stripe"
    }
  }
}

# The key comes from the STRIPE_API_KEY environment variable.
provider "stripe" {}
