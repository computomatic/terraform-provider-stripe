variable "connected_account" {
  type        = string
  description = "The connected account the object lives on."
}

provider "stripe" {
  alias          = "connected"
  stripe_account = var.connected_account
}

data "stripe_treasury_financial_account" "operating" {
  provider = stripe.connected
  id       = "fa_1MtYCCLkdIwHu7ixUqlIQr8T"
}

output "financial_account_balance" {
  value = data.stripe_treasury_financial_account.operating.balance.cash
}
