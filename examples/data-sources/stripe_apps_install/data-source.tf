data "stripe_apps_install" "reporting" {
  id = "appinst_1Nv0FGQ9RKHgCVdK"
}

output "reporting_permissions" {
  value = data.stripe_apps_install.reporting.permissions_granted
}
