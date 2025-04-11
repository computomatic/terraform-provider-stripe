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

# data "stripe_product" "existing" {
#   id = "prod_S6FuCl4cN7DvwU"
# }

resource "stripe_product" "new" {
  name = "New Product"
  description = "Created by Terraform for testing"
  active = true
}

# output "existing_product" {
#   value = data.stripe_product.existing
# }

# output "new_product" {
#   value = stripe_product.new
# }
