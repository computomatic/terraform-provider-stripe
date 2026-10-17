data "stripe_balance" "current" {}

output "available_usd" {
  value = [for b in data.stripe_balance.current.available : b.amount if b.currency == "usd"]
}
