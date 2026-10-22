data "stripe_review" "flagged" {
  id = "prv_123"
}

output "review_reason" {
  value = data.stripe_review.flagged.reason
}
