data "stripe_commerce_product_catalog_import" "spring" {
  id = "pci_123"
}

output "import_status" {
  value = data.stripe_commerce_product_catalog_import.spring.status
}
