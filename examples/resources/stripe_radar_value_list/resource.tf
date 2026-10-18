resource "stripe_radar_value_list" "blocked_emails" {
  alias     = "blocked_emails"
  name      = "Blocked email addresses"
  item_type = "email"
}
