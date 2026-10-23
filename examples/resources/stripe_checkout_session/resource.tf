resource "stripe_product" "seat" {
  name = "Seat"
}

resource "stripe_price" "seat" {
  product     = stripe_product.seat.id
  currency    = "usd"
  unit_amount = 2000
}

# A hosted checkout page for two seats. Send the customer to its `url`.
resource "stripe_checkout_session" "order" {
  mode        = "payment"
  success_url = "https://example.com/thanks"
  cancel_url  = "https://example.com/cart"

  line_items = [{
    price    = stripe_price.seat.id
    quantity = 2
  }]
}

output "checkout_url" {
  value = stripe_checkout_session.order.url
}
