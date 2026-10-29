resource "stripe_file" "evidence" {
  source  = "${path.module}/receipt.png"
  purpose = "dispute_evidence"
}

output "file_url" {
  value = stripe_file.evidence.url
}
