package main

// Resources lists the objects the generator writes tables for. A resource joins the list
// when its declaration is written.
var Resources = []Resource{
	{Package: "balance", ReadPath: "/v1/balance"},
	{Package: "billing_meter", CreatePath: "/v1/billing/meters", UpdatePath: "/v1/billing/meters/{id}"},
	{Package: "billing_portal_configuration", CreatePath: "/v1/billing_portal/configurations", UpdatePath: "/v1/billing_portal/configurations/{configuration}"},
	{Package: "country_spec", ReadPath: "/v1/country_specs/{country}", Omit: []string{"supported_bank_account_currencies"}},
	{Package: "coupon", CreatePath: "/v1/coupons", UpdatePath: "/v1/coupons/{coupon}"},
	{Package: "customer", CreatePath: "/v1/customers", UpdatePath: "/v1/customers/{customer}", Omit: []string{"sources", "subscriptions"}},
	{Package: "entitlements_feature", CreatePath: "/v1/entitlements/features", UpdatePath: "/v1/entitlements/features/{id}"},
	{Package: "plan", CreatePath: "/v1/plans", UpdatePath: "/v1/plans/{plan}"},
	{Package: "price", CreatePath: "/v1/prices", UpdatePath: "/v1/prices/{price}"},
	{Package: "product", CreatePath: "/v1/products", UpdatePath: "/v1/products/{id}"},
	{Package: "product_catalog_trial_offer", CreatePath: "/v1/product_catalog/trial_offers", UpdatePath: "/v1/product_catalog/trial_offers/{id}"},
	{Package: "product_feature", CreatePath: "/v1/products/{product}/features"},
	{Package: "promotion_code", CreatePath: "/v1/promotion_codes", UpdatePath: "/v1/promotion_codes/{promotion_code}"},
	{Package: "shipping_rate", CreatePath: "/v1/shipping_rates", UpdatePath: "/v1/shipping_rates/{shipping_rate_token}"},
	{Package: "tax_code", ReadPath: "/v1/tax_codes/{id}"},
	{Package: "tax_rate", CreatePath: "/v1/tax_rates", UpdatePath: "/v1/tax_rates/{tax_rate}"},
	{Package: "webhook_endpoint", CreatePath: "/v1/webhook_endpoints", UpdatePath: "/v1/webhook_endpoints/{webhook_endpoint}"},
}
