data "stripe_identity_verification_session" "kyc" {
  id = "vs_123"
}

output "verification_status" {
  value = data.stripe_identity_verification_session.kyc.status
}
