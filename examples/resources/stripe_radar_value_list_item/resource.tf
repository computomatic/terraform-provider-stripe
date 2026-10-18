resource "stripe_radar_value_list" "blocked_emails" {
  alias     = "blocked_emails"
  name      = "Blocked email addresses"
  item_type = "email"
}

resource "stripe_radar_value_list_item" "spammer" {
  value_list = stripe_radar_value_list.blocked_emails.id
  value      = "spammer@example.com"
}
