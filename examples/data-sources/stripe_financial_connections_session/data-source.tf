data "stripe_financial_connections_session" "link" {
  id = "fcsess_1MwVK82eZvKYlo2CBTW4ZPnf"
}

output "session_permissions" {
  value = data.stripe_financial_connections_session.link.permissions
}
