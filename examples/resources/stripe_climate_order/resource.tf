data "stripe_climate_product" "removal" {
  id = "climsku_123"
}

# Spends your available balance. Setting `status = "canceled"` cancels the order and refunds
# the removal, but not Stripe's fees. Stripe cannot delete an order, so destroy is refused.
resource "stripe_climate_order" "offset" {
  product     = data.stripe_climate_product.removal.id
  metric_tons = "1"

  beneficiary = {
    public_name = "Acme Inc."
  }
}
