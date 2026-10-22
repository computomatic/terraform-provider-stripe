data "stripe_identity_verification_report" "onboarding" {
  id = "vr_123"
}

output "document_status" {
  value     = data.stripe_identity_verification_report.onboarding.document.status
  sensitive = true
}
