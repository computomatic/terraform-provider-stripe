data "stripe_apps_secret" "api_token" {
  name = "api_token"
  scope = {
    type = "account"
  }
}

output "api_token" {
  value     = data.stripe_apps_secret.api_token.payload
  sensitive = true
}
