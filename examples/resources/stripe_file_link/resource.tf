# Files are uploaded to Stripe outside Terraform; refer to one by its ID.
data "stripe_file" "report" {
  id = "file_1AbCdEf"
}

resource "stripe_file_link" "report" {
  file       = data.stripe_file.report.id
  expires_at = "1893456000" # Unix time, 2030-01-01
}
