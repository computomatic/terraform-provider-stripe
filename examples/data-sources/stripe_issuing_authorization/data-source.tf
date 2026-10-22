variable "connected_account" {
  type        = string
  description = "The connected account the object lives on."
}

provider "stripe" {
  alias          = "connected"
  stripe_account = var.connected_account
}

data "stripe_issuing_authorization" "purchase" {
  provider = stripe.connected
  id       = "iauth_1MvSieLkdIwHu7ixR3pYIXcP"
}

output "authorization_approved" {
  value = data.stripe_issuing_authorization.purchase.approved
}
