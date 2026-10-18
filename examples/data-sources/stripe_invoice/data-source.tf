# The invoice a subscription created.
data "stripe_invoice" "latest" {
  id = stripe_subscription.team.latest_invoice
}
