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

resource "stripe_product" "example_product" {
  name = "Example Product"
  description = "Created by Terraform for testing"
}

resource "stripe_price" "example_price" {
  product = stripe_product.example_product.id
  unit_amount = 1000
  currency = "usd"
}

resource "stripe_payment_link" "example_link" {
  line_items = [
    {
      price = stripe_price.example_price.id
      quantity = 1
      adjustable_quantity = {
        enabled = true
        minimum = 1
        maximum = 10
      }
    }
  ]
}

output "payment_link" {
  value = stripe_payment_link.example_link
}
