resource "stripe_product" "course" {
  name = "Terraform course"
}

resource "stripe_price" "course" {
  product     = stripe_product.course.id
  currency    = "usd"
  unit_amount = 4900
}

resource "stripe_payment_link" "course" {
  line_items = [{
    price    = stripe_price.course.id
    quantity = 1
  }]

  allow_promotion_codes = true

  after_completion = {
    type = "redirect"
    redirect = {
      url = "https://example.com/thanks"
    }
  }
}

output "checkout_url" {
  value = stripe_payment_link.course.url
}
