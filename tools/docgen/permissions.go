package main

import (
	"fmt"
	"slices"
	"strings"
)

// permissionsMarker is replaced, in the guide that holds it, by the table of the restricted-key
// permissions each resource and data source needs.
const permissionsMarker = "<!-- permissions-table -->"

// permission is what a restricted key needs to use one type. The names are those of Stripe's
// API permissions reference (https://docs.stripe.com/keys/permissions-reference), which gives
// the permission of each endpoint; an entry lists the permissions of the endpoints in the
// type's declaration. A `_write` permission includes the `_read` of the same name, so Write
// lists a read permission only when its name differs.
type permission struct {
	// Read is what the data source needs: the retrieve, list and parent endpoints.
	Read []string
	// Write is what the resource needs, its refresh and import included. It is empty for a
	// type with no resource, and for one whose writes the reference does not list.
	Write []string
	// Unverified lists the endpoints the type calls that the reference does not list.
	Unverified []string
	// Note says what else the type reads.
	Note string
}

// directPermissions are the permissions the reference marks (Direct): they cannot authorize a
// request made on a connected account.
var directPermissions = map[string]bool{
	"application_fee_read":    true,
	"application_fee_write":   true,
	"connected_account_read":  true,
	"connected_account_write": true,
	"top_up_read":             true,
	"top_up_write":            true,
	"transfer_read":           true,
	"transfer_write":          true,
}

// permissions is keyed by type name. Every shipped type needs an entry, or the docs do not
// generate. Read the reference as of 2026-10-23.
var permissions = map[string]permission{
	"stripe_account":                                 {Read: []string{"connected_account_read"}, Write: []string{"connected_account_write"}, Unverified: []string{"POST /v1/accounts"}},
	"stripe_account_capability":                      {Read: []string{"connected_account_read"}},
	"stripe_account_external_account":                {Read: []string{"connected_account_read"}, Write: []string{"connected_account_write"}},
	"stripe_account_person":                          {Read: []string{"connected_account_read"}, Write: []string{"connected_account_write"}},
	"stripe_apple_pay_domain":                        {Unverified: []string{"POST /v1/apple_pay/domains", "GET /v1/apple_pay/domains/{id}", "DELETE /v1/apple_pay/domains/{id}", "GET /v1/apple_pay/domains"}},
	"stripe_application_fee":                         {Read: []string{"application_fee_read"}},
	"stripe_application_fee_refund":                  {Read: []string{"application_fee_read"}, Write: []string{"application_fee_write"}},
	"stripe_apps_install":                            {Read: []string{"app_install_read"}},
	"stripe_apps_secret":                             {Read: []string{"secret_read"}},
	"stripe_balance":                                 {Read: []string{"balance_read"}},
	"stripe_balance_settings":                        {Read: []string{"v2_account_storer_read"}},
	"stripe_balance_transaction":                     {Read: []string{"balance_read"}},
	"stripe_billing_alert":                           {Read: []string{"billing_alert_read"}, Write: []string{"billing_alert_write"}},
	"stripe_billing_credit_balance_transaction":      {Read: []string{"billing_credit_balance_read"}},
	"stripe_billing_credit_grant":                    {Read: []string{"billing_credit_grant_read"}, Write: []string{"billing_credit_grant_write"}},
	"stripe_billing_feedback_option":                 {Read: []string{"subscription_read"}, Write: []string{"subscription_write"}},
	"stripe_billing_meter":                           {Read: []string{"billing_meter_read"}, Write: []string{"billing_meter_write"}},
	"stripe_billing_meter_event_summary":             {Read: []string{"billing_meter_event_read"}},
	"stripe_billing_portal_configuration":            {Read: []string{"customer_portal_read"}, Write: []string{"customer_portal_write"}},
	"stripe_charge":                                  {Read: []string{"charge_read"}, Write: []string{"charge_write"}, Unverified: []string{"POST /v1/charges"}},
	"stripe_checkout_session":                        {Read: []string{"checkout_session_read"}, Write: []string{"checkout_session_write"}},
	"stripe_checkout_session_line_item":              {Read: []string{"checkout_session_read"}},
	"stripe_climate_order":                           {Read: []string{"climate_order_read"}, Write: []string{"climate_order_write"}},
	"stripe_climate_product":                         {Read: []string{"climate_order_read"}},
	"stripe_climate_supplier":                        {Read: []string{"climate_order_read"}},
	"stripe_commerce_product_catalog_import":         {Read: []string{"product_catalog_import_read"}},
	"stripe_core_account":                            {Read: []string{"v2_account_read"}, Write: []string{"v2_account_read", "v2_account_storer_write"}},
	"stripe_core_account_person":                     {Read: []string{"v2_account_read"}, Write: []string{"v2_account_read", "v2_account_storer_write"}},
	"stripe_core_event":                              {Read: []string{"event_read"}, Unverified: []string{"GET /v2/core/events/{id}"}},
	"stripe_core_event_destination":                  {Read: []string{"webhook_read"}, Write: []string{"webhook_write"}},
	"stripe_country_spec":                            {Unverified: []string{"GET /v1/country_specs/{id}"}},
	"stripe_coupon":                                  {Read: []string{"coupon_read"}, Write: []string{"coupon_write"}},
	"stripe_credit_note":                             {Read: []string{"credit_note_read"}, Write: []string{"credit_note_write"}},
	"stripe_credit_note_line":                        {Read: []string{"credit_note_read"}},
	"stripe_customer":                                {Read: []string{"customer_read"}, Write: []string{"customer_write"}, Unverified: []string{"POST /v1/customers/{id}"}},
	"stripe_customer_balance_transaction":            {Read: []string{"customer_read"}, Write: []string{"customer_write"}},
	"stripe_customer_bank_account":                   {Unverified: []string{"GET /v1/customers/{customer}/bank_accounts/{id}", "GET /v1/customers/{customer}/bank_accounts"}},
	"stripe_customer_card":                           {Unverified: []string{"GET /v1/customers/{customer}/cards/{id}", "GET /v1/customers/{customer}/cards"}},
	"stripe_customer_cash_balance_transaction":       {Read: []string{"customer_read"}},
	"stripe_customer_payment_method":                 {Read: []string{"payment_method_read"}, Write: []string{"payment_method_write"}},
	"stripe_dispute":                                 {Read: []string{"dispute_read"}},
	"stripe_entitlements_active_entitlement":         {Read: []string{"entitlement_read"}},
	"stripe_entitlements_feature":                    {Read: []string{"feature_read"}, Write: []string{"feature_write"}},
	"stripe_event":                                   {Read: []string{"event_read"}},
	"stripe_file":                                    {Read: []string{"file_read"}},
	"stripe_file_link":                               {Read: []string{"file_read"}, Write: []string{"file_write"}},
	"stripe_financial_connections_account":           {Read: []string{"connections_api_accounts_list_read", "connections_api_accounts_retrieve_read"}},
	"stripe_financial_connections_account_owner":     {Read: []string{"connections_api_accounts_ownership_read", "connections_api_accounts_retrieve_read"}, Note: "Also reads the account the owners belong to."},
	"stripe_financial_connections_session":           {Read: []string{"connections_api_link_account_sessions_read"}, Unverified: []string{"GET /v1/financial_connections/sessions"}},
	"stripe_financial_connections_transaction":       {Read: []string{"connections_api_transactions_read"}},
	"stripe_identity_verification_report":            {Read: []string{"identity_product_read"}},
	"stripe_identity_verification_session":           {Read: []string{"identity_product_read"}, Write: []string{"identity_product_write"}},
	"stripe_invoice":                                 {Read: []string{"invoice_read"}, Write: []string{"invoice_write"}},
	"stripe_invoice_line":                            {Read: []string{"invoice_read"}},
	"stripe_invoice_payment":                         {Read: []string{"invoice_read"}},
	"stripe_invoice_rendering_template":              {Read: []string{"invoice_rendering_template_read"}},
	"stripe_invoiceitem":                             {Read: []string{"invoice_read"}, Write: []string{"invoice_write"}},
	"stripe_issuing_authorization":                   {Read: []string{"issuing_authorization_read"}},
	"stripe_issuing_card":                            {Read: []string{"issuing_card_read"}, Write: []string{"issuing_card_write"}, Unverified: []string{"GET /v1/issuing/cards/{id}"}},
	"stripe_issuing_cardholder":                      {Read: []string{"issuing_cardholder_read"}, Write: []string{"issuing_cardholder_write"}},
	"stripe_issuing_dispute":                         {Read: []string{"issuing_dispute_read"}, Unverified: []string{"POST /v1/issuing/disputes", "POST /v1/issuing/disputes/{id}", "POST /v1/issuing/disputes/{id}/submit"}},
	"stripe_issuing_personalization_design":          {Read: []string{"issuing_card_read"}, Write: []string{"issuing_card_write"}},
	"stripe_issuing_physical_bundle":                 {Read: []string{"issuing_card_read"}},
	"stripe_issuing_token":                           {Read: []string{"issuing_token_read"}, Unverified: []string{"GET /v1/issuing/tokens/{id}"}},
	"stripe_issuing_transaction":                     {Read: []string{"issuing_transaction_read"}},
	"stripe_mandate":                                 {Read: []string{"mandate_read"}},
	"stripe_payment_intent":                          {Read: []string{"payment_intent_read"}, Write: []string{"payment_intent_write"}, Unverified: []string{"POST /v1/payment_intents", "GET /v1/payment_intents/{id}", "POST /v1/payment_intents/{id}", "POST /v1/payment_intents/{id}/confirm", "POST /v1/payment_intents/{id}/cancel"}},
	"stripe_payment_intent_amount_details_line_item": {Read: []string{"payment_intent_read"}},
	"stripe_payment_link":                            {Read: []string{"payment_links_read"}, Write: []string{"payment_links_write"}},
	"stripe_payment_link_line_item":                  {Read: []string{"payment_links_read"}},
	"stripe_payment_method_configuration":            {Write: []string{"payment_method_configurations_write"}, Unverified: []string{"GET /v1/payment_method_configurations/{id}", "GET /v1/payment_method_configurations"}},
	"stripe_payment_method_domain":                   {Read: []string{"payment_method_domain_read"}, Write: []string{"payment_method_domain_write"}},
	"stripe_payment_record":                          {Read: []string{"payment_records_read"}},
	"stripe_payout":                                  {Read: []string{"payout_read"}, Write: []string{"payout_write"}},
	"stripe_plan":                                    {Read: []string{"plan_read"}, Write: []string{"plan_write"}},
	"stripe_price":                                   {Read: []string{"plan_read"}, Write: []string{"plan_write"}},
	"stripe_product":                                 {Read: []string{"product_read"}, Write: []string{"product_write"}},
	"stripe_product_catalog_trial_offer":             {Write: []string{"subscription_write"}, Unverified: []string{"POST /v1/product_catalog/trial_offers", "GET /v1/product_catalog/trial_offers/{id}", "GET /v1/product_catalog/trial_offers"}},
	"stripe_product_feature":                         {Read: []string{"product_read"}, Write: []string{"product_write"}},
	"stripe_promotion_code":                          {Read: []string{"promotion_code_read"}, Write: []string{"promotion_code_write"}},
	"stripe_quote":                                   {Read: []string{"quote_read"}, Write: []string{"quote_write"}},
	"stripe_quote_computed_upfront_line_item":        {Read: []string{"quote_read"}},
	"stripe_quote_line_item":                         {Read: []string{"quote_read"}},
	"stripe_radar_early_fraud_warning":               {Read: []string{"charge_read"}},
	"stripe_radar_value_list":                        {Read: []string{"radar_value_list_read"}, Write: []string{"radar_value_list_write"}},
	"stripe_radar_value_list_item":                   {Read: []string{"radar_value_list_read"}, Write: []string{"radar_value_list_write"}},
	"stripe_refund":                                  {Read: []string{"charge_read"}, Write: []string{"charge_write"}, Unverified: []string{"POST /v1/refunds"}},
	"stripe_reporting_report_run":                    {Read: []string{"report_runs_and_report_types_read"}},
	"stripe_reporting_report_type":                   {Read: []string{"report_runs_and_report_types_read"}},
	"stripe_review":                                  {Read: []string{"review_read"}},
	"stripe_setup_intent":                            {Read: []string{"setup_intent_read"}, Write: []string{"setup_intent_write"}, Unverified: []string{"POST /v1/setup_intents", "GET /v1/setup_intents/{id}", "POST /v1/setup_intents/{id}/cancel"}},
	"stripe_shipping_rate":                           {Read: []string{"shipping_rate_read"}, Write: []string{"shipping_rate_write"}},
	"stripe_subscription":                            {Read: []string{"subscription_read"}, Write: []string{"subscription_write"}},
	"stripe_subscription_item":                       {Read: []string{"subscription_read"}, Write: []string{"subscription_write"}},
	"stripe_subscription_schedule":                   {Read: []string{"subscription_read"}, Write: []string{"subscription_write"}},
	"stripe_tax_code":                                {Read: []string{"quote_read"}},
	"stripe_tax_id":                                  {Read: []string{"tax_id_read"}, Write: []string{"tax_id_write"}},
	"stripe_tax_location":                            {Read: []string{"tax_locations_read"}},
	"stripe_tax_rate":                                {Read: []string{"tax_rate_read"}, Write: []string{"tax_rate_write"}},
	"stripe_tax_registration":                        {Read: []string{"tax_settings_read"}, Write: []string{"tax_settings_write"}},
	"stripe_tax_settings":                            {Read: []string{"tax_settings_read"}},
	"stripe_tax_transaction":                         {Read: []string{"tax_calculations_and_transactions_read"}},
	"stripe_tax_transaction_line_item":               {Read: []string{"tax_calculations_and_transactions_read"}},
	"stripe_terminal_configuration":                  {Read: []string{"terminal_configuration_read"}, Write: []string{"terminal_configuration_write"}},
	"stripe_terminal_location":                       {Read: []string{"terminal_location_read"}, Write: []string{"terminal_location_write"}},
	"stripe_terminal_reader":                         {Read: []string{"terminal_reader_read"}, Write: []string{"terminal_reader_write"}},
	"stripe_test_helpers_test_clock":                 {Read: []string{"billing_clock_read"}, Write: []string{"billing_clock_write"}},
	"stripe_topup":                                   {Read: []string{"top_up_read"}, Write: []string{"top_up_write"}},
	"stripe_transfer":                                {Write: []string{"transfer_write"}, Unverified: []string{"GET /v1/transfers/{id}", "POST /v1/transfers/{id}", "GET /v1/transfers"}},
	"stripe_transfer_reversal":                       {Read: []string{"transfer_read"}, Write: []string{"transfer_write"}, Unverified: []string{"POST /v1/transfers/{transfer}/reversals"}},
	"stripe_treasury_credit_reversal":                {Read: []string{"treasury_credit_reversal_read"}},
	"stripe_treasury_debit_reversal":                 {Read: []string{"treasury_debit_reversal_read"}},
	"stripe_treasury_financial_account":              {Read: []string{"financial_account_read_treasury"}, Write: []string{"financial_account_read_treasury", "treasury_financial_account_write"}},
	"stripe_treasury_inbound_transfer":               {Read: []string{"treasury_inbound_transfer_read"}},
	"stripe_treasury_outbound_payment":               {Read: []string{"treasury_outbound_payment_read"}},
	"stripe_treasury_outbound_transfer":              {Read: []string{"treasury_outbound_transfer_read"}},
	"stripe_treasury_received_credit":                {Read: []string{"treasury_received_credit_read"}},
	"stripe_treasury_received_debit":                 {Read: []string{"treasury_received_debit_read"}},
	"stripe_treasury_transaction":                    {Read: []string{"treasury_transaction_read"}},
	"stripe_treasury_transaction_entry":              {Read: []string{"treasury_transaction_read"}},
	"stripe_webhook_endpoint":                        {Read: []string{"webhook_read"}, Write: []string{"webhook_write"}},
}

// permissionTable renders the table of what each shipped type needs. It fails when a type has
// no entry, an entry names no shipped type, or an entry has no permission for a part of the
// type and does not say which endpoints the reference leaves out.
func permissionTable(s *schemas, entries map[string]permission) (string, error) {
	resources := map[string]*schema{}
	for _, r := range s.Resources {
		resources[r.Type] = r
	}
	var types []string
	for _, d := range s.DataSources {
		types = append(types, d.Type)
	}
	for _, r := range s.Resources {
		if !slices.Contains(types, r.Type) {
			types = append(types, r.Type)
		}
	}
	slices.Sort(types)
	for name := range entries {
		if !slices.Contains(types, name) {
			return "", fmt.Errorf("permissions: %s is not a shipped resource or data source", name)
		}
	}
	rows := []string{"| Type | Data source | Resource | Notes |", "| --- | --- | --- | --- |"}
	for _, name := range types {
		p, ok := entries[name]
		if !ok {
			return "", fmt.Errorf("permissions: %s has no entry; add the permissions Stripe's API permissions reference gives its endpoints", name)
		}
		r := resources[name]
		unverified := len(p.Unverified) > 0
		if len(p.Read) == 0 && !unverified || r != nil && len(p.Write) == 0 && !unverified {
			return "", fmt.Errorf("permissions: %s has no permission and lists no unverified endpoint", name)
		}
		if r == nil && len(p.Write) > 0 {
			return "", fmt.Errorf("permissions: %s has no resource, so it needs no Write", name)
		}
		read := permissionCell(p.Read)
		var write string
		var notes []string
		switch {
		case r == nil:
			write = "No resource"
		case r.MoneyNote != "":
			write = permissionCell(append(slices.Clone(p.Write), "balance_read"))
			notes = append(notes, "`balance_read` is for the live-mode check.")
		default:
			write = permissionCell(p.Write)
		}
		if p.Note != "" {
			notes = append(notes, p.Note)
		}
		if unverified {
			notes = append(notes, "Unverified: the reference does not list "+codeList(p.Unverified)+".")
		}
		rows = append(rows, fmt.Sprintf("| `%s` | %s | %s | %s |", name, read, write, strings.Join(notes, " ")))
	}
	return strings.Join(rows, "\n"), nil
}

// permissionCell lists permissions, each marked (Direct) as the reference marks it. A part
// with none is unverified: the type's entry says which endpoints the reference leaves out.
func permissionCell(names []string) string {
	if len(names) == 0 {
		return "Unverified"
	}
	cells := make([]string, len(names))
	for i, n := range names {
		cells[i] = "`" + n + "`"
		if directPermissions[n] {
			cells[i] += " (Direct)"
		}
	}
	return strings.Join(cells, ", ")
}

// codeList joins values as code spans: "`a`", "`a` and `b`", "`a`, `b` and `c`".
func codeList(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = "`" + v + "`"
	}
	if len(quoted) == 1 {
		return quoted[0]
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " and " + quoted[len(quoted)-1]
}
