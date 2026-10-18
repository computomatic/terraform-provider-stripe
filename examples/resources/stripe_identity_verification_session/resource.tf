resource "stripe_customer" "applicant" {
  email = "applicant@example.com"
}

# Send the person to the session's `url`, or hand the `client_secret` to Stripe.js.
resource "stripe_identity_verification_session" "kyc" {
  type             = "document"
  related_customer = stripe_customer.applicant.id

  options = {
    document = {
      allowed_types        = ["passport", "driving_license"]
      require_live_capture = true
    }
  }
}

output "verification_url" {
  value     = stripe_identity_verification_session.kyc.url
  sensitive = true
}
