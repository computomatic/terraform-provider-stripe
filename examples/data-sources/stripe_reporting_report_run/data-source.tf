data "stripe_reporting_report_run" "balance" {
  id = "frr_123"
}

output "report_url" {
  value = data.stripe_reporting_report_run.balance.result.url
}
