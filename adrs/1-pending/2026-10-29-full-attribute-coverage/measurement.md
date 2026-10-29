# Attribute coverage: measurement

Measured 2026-10-28 at provider commit `0832bd8`, against the pinned spec `spec/openapi.json` (`stripe/openapi` tag `v2585`, API version `2026-09-30.endive`). The script is `measure_test.go.txt` beside this file: copy it to `tools/schemagen/measure_test.go` and run `MEASURE_OUT=out.md go test ./tools/schemagen -run TestMeasure`. Everything under the heading "Generated" is its output, unedited.

## Method

- **Tables.** Every entry of `tools/schemagen/resources.go` at `0832bd8`: 121 tables, 62 with a resource and all 121 with a data source. Table names are the generator's package names; the `/v2` tables take their new type names from provider issue #66 (`core_account` is `stripe_account_v2`, `core_account_person` is `stripe_account_person_v2`, `core_event_destination` is `stripe_event_destination_v2`, `core_event` is `stripe_event_v2`).
- **Spec leaves.** For each table, the script walks the same operations the generator reads (the create and update request bodies, and the create, retrieve or list-element response, with the `Response` and `CreateHash` overrides) through `$ref`, `anyOf`, `oneOf`, `allOf`, arrays and maps, and counts leaves: scalars, scalar arrays, free-form maps, and hashes with no keys. An array or map of hashes adds no path segment, as in the generated tables. Request leaves are the union of the create and update bodies. A schema met again on its own path stops the walk and counts as one leaf (recursion). An expandable reference (`x-expansionResources`) counts as one leaf, its ID, as the architecture ADR's D2 maps it.
- **Exposed leaves.** The leaves of the generated table (`tools/schemagen.Build`), which is what the engine turns into attributes. A request leaf is exposed when the table marks it `Create` or `Update`; a response leaf when it marks it `Response`. A data source exposes the response leaves.
- **Reasons.** Each missing leaf gets the first reason that applies, in this order:

|Reason|Rule|
|-|-|
|request control|top-level `expand` or `include`|
|deprecated|the schema says `deprecated: true` or its description says "deprecated"|
|embedded copy of another object|a response leaf inside a nested schema that is itself an API object (`x-resourceId`), not expandable, at a path the request does not write (an error hash's `payment_intent`, a customer's `subscriptions`)|
|ID in, object out|as above, but the request sends the same path as an ID (a subscription item's `price`)|
|objects in, IDs out|on a `discounts` path|
|recursion|the walk met a schema already on its path|
|union|below an `anyOf` or `oneOf` with more than one non-empty, non-reference variant (a polymorphic `source`, `payment_method_options.card` in a payment intent's response)|
|empty object|a hash with no keys (a payment method type marker)|
|inline data for another object|a request leaf the response does not have, below a `*_data` parameter (`payment_method_data`, `price_data`)|
|request-only instruction|any other top-level request leaf the response does not have (`confirm`, `proration_behavior`, `account_token`)|
|write-only value|any other nested request leaf the response does not have (`company.tax_id`, returned only as `company.tax_id_provided`)|
|trimmed for lack of proof|everything else: the table left it out by `Keep`, `Omit`, `ReadOnly` or `NoResponse` because the conformance suite has not proven it|

- **Defaults.** "Default in text" counts exposed optional argument leaves whose description (the one the docs show, from the request or else the response) contains the word "default". The spec has no `default` keyword on any parameter schema: the 644 `"default":` keys in `spec/openapi.json` are each operation's default (error) response.
- **Limits.** The reasons are heuristics over the spec's shapes; a leaf can have several and gets the first. "Write-only value" includes a few leaves that Stripe returns elsewhere (a payment intent's `amount_details.line_items`, returned by their own list). The counts are leaves, not attributes: `company.address` is six leaves.

## Headlines

- **Request parameters:** the spec has 3,238 leaves across the 62 resources' create and update bodies; the provider exposes 1,971 (61%). Missing: 1,267, of which 725 were trimmed for lack of proof.
- **Response fields:** the spec has 18,875 leaves across all 121 tables; the provider exposes 9,120 (48%). Missing: 9,940, of which 7,694 (77%) sit inside embedded copies of other objects (an invoice's `last_finalization_error.payment_intent` alone is about 1,000) and 740 were trimmed for lack of proof.
- **Already exposed inside embedded copies:** 3,502 leaves in 29 tables, such as a Treasury transaction's `flow_details` (651) and a terminal reader's `action` (402). The provider already reads embedded copies where it does not omit them, so representing every embedded copy only by its ID would remove attributes that ship today.
- **Defaults:** 1,450 optional argument leaves are exposed; 147 of their descriptions mention a default and the generator marks 222 server-defaulted. Of the spec's 3,177 request leaves (controls aside), 87 carry the word "default" in their own description. The spec text cannot document defaults on its own.
- **Ten largest gaps** (request plus response leaves): `invoice` 2,028; `setup_intent` 1,916; `payment_intent` 1,867; `terminal_reader` 1,823; `treasury_transaction` 669 (data source); `core_account` 621; `customer` 599; `account` 454; `payment_link` 109; `subscription` 108. Without embedded copies the order is `payment_intent` 1,349, `core_account` 621, `account` 362, `setup_intent` 335, `core_account_person` 102, `subscription_schedule` 89, `account_person` 88, `issuing_card` 86, `subscription_item` 65 and `customer_payment_method` 47.

## `stripe_account` in short

The create request (`POST /v1/accounts`) has 19 top-level parameters; the response (`account`) carries 382 leaves. The provider exposes 108 of 281 request leaves and 101 of 382 response leaves.

- **`company`:** the spec's create request lists 25 properties under `company`; the table keeps one, `name` (`tools/schemagen/resources.go:108` at `0832bd8`), and does not read it back (`NoResponse`, line 119). Missing: 60 request leaves and 62 response leaves (addresses in three scripts, declarations, verification, structure, registration, `tax_id`, `vat_id` and `registration_number`, which Stripe returns only as `tax_id_provided` and `vat_id_provided`).
- **`individual`:** 17 leaves kept for a US custom individual, sent on create, never read back; 40 request leaves missing. The response's `individual` is a whole `person` object, of which 95 leaves are missing (most counted as an embedded copy, because the request does not write those paths, such as its `requirements`).
- **`settings` and `business_profile`:** present in the response and readable; their request leaves, with `default_currency`, are read-only by `ReadOnly` (47 leaves) because the suite has not proven the updates.
- **`capabilities`:** requested flags in the request, statuses in the response; the resource keeps the request shape and does not read it (69 response leaves).
- **Not offered:** `documents` (file IDs, 11 leaves), `bank_account` (a token or a hash, a union), `account_token`, `groups`, `tos_acceptance.service_agreement` and `user_agent`, and `external_accounts` in the response (a paged list of bank accounts and cards).

The full breakdown by top-level field is the last table below.

## Generated

Tables: 121 (62 with a resource, 121 with a data source)

|Side|Spec leaves|Exposed|Missing|
|-|-|-|-|
|Request parameters|3238|1971|1267|
|Response fields|18875|9120|9940|

Request leaves in the spec: 3177; with "default" in their own description: 87

Optional argument leaves exposed: 1450; with "default" in the spec text: 147; marked server-defaulted by the generator: 222

|Reason|Request leaves|Response leaves|
|-|-|-|
|trimmed for lack of proof|725|740|
|embedded copy of another object|0|7694|
|union|63|1171|
|recursion|0|2|
|ID in, object out|0|221|
|objects in, IDs out|18|5|
|inline data for another object|123|0|
|write-only value|151|0|
|request-only instruction|28|0|
|request control|61|0|
|deprecated|1|1|
|empty object|97|106|

## Exposed today inside embedded copies of other objects

- customer: 37 leaves under tax_ids, discount, cash_balance
- file: 9 leaves under links
- radar_value_list: 7 leaves under list_items
- credit_note: 19 leaves under shipping_cost
- invoice: 79 leaves under lines, shipping_cost, payments
- quote: 190 leaves under computed, total_details
- subscription: 88 leaves under pending_update
- setup_intent: 201 leaves under last_setup_error
- payment_intent: 201 leaves under last_payment_error
- charge: 61 leaves under refunds
- dispute: 20 leaves under balance_transactions
- checkout_session: 52 leaves under total_details, shipping_cost
- terminal_reader: 402 leaves under action
- climate_order: 11 leaves under delivery_details
- climate_product: 11 leaves under suppliers
- credit_note_line: 19 leaves under tax_rates
- quote_line_item: 74 leaves under discounts, price, taxes
- quote_computed_upfront_line_item: 74 leaves under discounts, price, taxes
- treasury_transaction: 651 leaves under flow_details
- treasury_transaction_entry: 651 leaves under flow_details
- treasury_received_credit: 137 leaves under linked_flows
- issuing_dispute: 20 leaves under balance_transactions
- issuing_authorization: 193 leaves under transactions, card, balance_transactions
- financial_connections_session: 107 leaves under accounts, bank_account_token
- reporting_report_run: 22 leaves under result
- transfer: 10 leaves under reversals
- application_fee: 8 leaves under refunds
- checkout_session_line_item: 74 leaves under price, discounts, taxes
- payment_link_line_item: 74 leaves under discounts, price, taxes

## Per table

|Table|Kind|Req spec|Req exposed|Req missing|Resp spec|Resp exposed|Resp missing|Optional args|Default in text|
|-|-|-|-|-|-|-|-|-|-|
|invoice|resource|102|95|7|2272|252|2021|76|13|
|setup_intent|resource|193|14|179|1997|260|1737|13|2|
|payment_intent|resource|367|39|328|1997|458|1539|34|3|
|terminal_reader|resource|5|4|1|2308|486|1822|3|0|
|treasury_transaction|data source only|0|0|0|1336|667|669|0|0|
|core_account|resource|276|13|263|421|63|358|8|0|
|customer|resource|70|46|24|665|90|575|41|7|
|account|resource|281|108|173|382|101|281|101|1|
|payment_link|resource|105|89|16|183|140|93|55|8|
|subscription|resource|142|132|10|309|226|98|110|18|
|checkout_session|resource|326|317|9|435|392|93|261|27|
|core_account_person|resource|74|22|52|77|27|50|16|0|
|quote|resource|52|48|4|366|309|94|37|7|
|subscription_schedule|resource|111|95|16|165|95|73|73|12|
|account_person|resource|71|22|49|94|55|39|19|0|
|issuing_card|resource|39|21|18|104|37|68|15|1|
|subscription_item|resource|22|18|4|73|14|61|12|3|
|charge|resource|50|26|24|481|449|32|22|3|
|treasury_financial_account|resource|18|14|4|71|25|46|4|0|
|customer_payment_method|resource|20|12|8|237|198|39|12|0|
|credit_note|resource|29|28|1|116|95|45|21|1|
|invoiceitem|resource|28|23|5|53|34|20|19|3|
|refund|resource|12|11|1|81|60|21|11|0|
|tax_transaction|data source only|0|0|0|49|33|16|0|0|
|account_external_account|resource|21|8|13|28|27|1|4|1|
|mandate|data source only|0|0|0|62|51|11|0|0|
|plan|resource|32|22|10|24|24|0|17|3|
|payment_record|data source only|0|0|0|335|326|9|0|0|
|identity_verification_session|resource|16|12|4|47|43|4|11|0|
|product_feature|resource|2|1|1|9|3|7|0|0|
|issuing_personalization_design|resource|12|9|3|17|17|0|8|0|
|tax_id|resource|7|4|3|16|16|0|1|0|
|application_fee_refund|resource|3|2|1|7|6|1|2|0|
|core_event|data source only|0|0|0|14|12|2|0|0|
|core_event_destination|resource|15|14|1|24|23|1|4|0|
|customer_balance_transaction|resource|5|4|1|14|13|1|2|0|
|event|data source only|0|0|0|12|10|2|0|0|
|issuing_dispute|resource|46|44|2|72|72|0|42|1|
|test_helpers_test_clock|resource|4|2|2|8|8|0|1|0|
|topup|resource|10|8|2|235|235|0|5|0|
|transfer_reversal|resource|5|4|1|9|8|1|4|1|
|account_capability|data source only|0|0|0|27|26|1|0|0|
|apple_pay_domain|resource|2|1|1|4|4|0|0|0|
|balance_settings|data source only|0|0|0|13|12|1|0|0|
|billing_alert|resource|8|7|1|10|10|0|1|0|
|billing_credit_grant|resource|14|13|1|19|19|0|9|3|
|billing_feedback_option|resource|2|1|1|5|5|0|0|0|
|billing_meter|resource|8|7|1|13|13|0|1|0|
|billing_meter_event_summary|data source only|0|0|0|6|5|1|0|0|
|billing_portal_configuration|resource|31|30|1|37|37|0|18|5|
|climate_order|resource|7|6|1|39|39|0|4|1|
|country_spec|data source only|0|0|0|10|9|1|0|0|
|coupon|resource|13|12|1|16|16|0|11|2|
|customer_bank_account|data source only|0|0|0|28|27|1|0|0|
|customer_card|data source only|0|0|0|30|29|1|0|0|
|customer_cash_balance_transaction|data source only|0|0|0|28|27|1|0|0|
|entitlements_feature|resource|5|4|1|6|6|0|2|0|
|file_link|resource|4|3|1|8|8|0|2|0|
|identity_verification_report|data source only|0|0|0|62|61|1|0|0|
|invoice_line|data source only|0|0|0|43|42|1|0|0|
|issuing_cardholder|resource|35|34|1|39|39|0|24|1|
|payment_method_configuration|resource|66|65|1|247|247|0|65|0|
|payment_method_domain|resource|3|2|1|17|17|0|1|0|
|payout|resource|10|9|1|27|27|0|7|1|
|price|resource|49|48|1|40|40|0|39|4|
|product|resource|43|42|1|22|22|0|31|2|
|product_catalog_trial_offer|resource|7|6|1|9|9|0|2|1|
|promotion_code|resource|14|13|1|17|17|0|12|0|
|radar_value_list|resource|5|4|1|18|18|0|2|0|
|radar_value_list_item|resource|3|2|1|6|6|0|0|0|
|shipping_rate|resource|15|14|1|17|17|0|5|1|
|tax_rate|resource|11|10|1|18|18|0|7|0|
|tax_registration|resource|178|177|1|159|159|0|32|8|
|tax_settings|data source only|0|0|0|13|12|1|0|0|
|terminal_configuration|resource|90|89|1|92|92|0|76|0|
|terminal_location|resource|27|26|1|28|28|0|25|0|
|transfer|resource|9|8|1|28|28|0|5|1|
|webhook_endpoint|resource|8|7|1|11|11|0|5|2|
|application_fee|data source only|0|0|0|26|26|0|0|0|
|apps_install|data source only|0|0|0|18|18|0|0|0|
|apps_secret|data source only|0|0|0|9|9|0|0|0|
|balance|data source only|0|0|0|41|41|0|0|0|
|balance_transaction|data source only|0|0|0|19|19|0|0|0|
|billing_credit_balance_transaction|data source only|0|0|0|19|19|0|0|0|
|checkout_session_line_item|data source only|0|0|0|90|90|0|0|0|
|climate_product|data source only|0|0|0|20|20|0|0|0|
|climate_supplier|data source only|0|0|0|10|10|0|0|0|
|commerce_product_catalog_import|data source only|0|0|0|25|25|0|0|0|
|credit_note_line|data source only|0|0|0|42|42|0|0|0|
|dispute|data source only|0|0|0|105|105|0|0|0|
|entitlements_active_entitlement|data source only|0|0|0|4|4|0|0|0|
|file|data source only|0|0|0|21|21|0|0|0|
|financial_connections_account|data source only|0|0|0|39|39|0|0|0|
|financial_connections_account_owner|data source only|0|0|0|7|7|0|0|0|
|financial_connections_session|data source only|0|0|0|125|125|0|0|0|
|financial_connections_transaction|data source only|0|0|0|12|12|0|0|0|
|invoice_payment|data source only|0|0|0|15|15|0|0|0|
|invoice_rendering_template|data source only|0|0|0|7|7|0|0|0|
|issuing_authorization|data source only|0|0|0|277|277|0|0|0|
|issuing_physical_bundle|data source only|0|0|0|8|8|0|0|0|
|issuing_token|data source only|0|0|0|36|36|0|0|0|
|issuing_transaction|data source only|0|0|0|67|67|0|0|0|
|payment_intent_amount_details_line_item|data source only|0|0|0|17|17|0|0|0|
|payment_link_line_item|data source only|0|0|0|90|90|0|0|0|
|quote_computed_upfront_line_item|data source only|0|0|0|90|90|0|0|0|
|quote_line_item|data source only|0|0|0|90|90|0|0|0|
|radar_early_fraud_warning|data source only|0|0|0|7|7|0|0|0|
|reporting_report_run|data source only|0|0|0|37|37|0|0|0|
|reporting_report_type|data source only|0|0|0|8|8|0|0|0|
|review|data source only|0|0|0|20|20|0|0|0|
|tax_code|data source only|0|0|0|4|4|0|0|0|
|tax_location|data source only|0|0|0|10|10|0|0|0|
|tax_transaction_line_item|data source only|0|0|0|12|12|0|0|0|
|treasury_credit_reversal|data source only|0|0|0|13|13|0|0|0|
|treasury_debit_reversal|data source only|0|0|0|14|14|0|0|0|
|treasury_inbound_transfer|data source only|0|0|0|37|37|0|0|0|
|treasury_outbound_payment|data source only|0|0|0|48|48|0|0|0|
|treasury_outbound_transfer|data source only|0|0|0|45|45|0|0|0|
|treasury_received_credit|data source only|0|0|0|173|173|0|0|0|
|treasury_received_debit|data source only|0|0|0|36|36|0|0|0|
|treasury_transaction_entry|data source only|0|0|0|665|665|0|0|0|

## Per table, missing by reason (request / response)

|Table|trimmed for lack of proof|embedded copy of another object|union|recursion|ID in, object out|objects in, IDs out|inline data for another object|write-only value|request-only instruction|request control|deprecated|empty object|
|-|-|-|-|-|-|-|-|-|-|-|-|-|
|invoice|0 / 12|0 / 1986|0 / 0|0 / 0|0 / 19|3 / 1|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|3 / 3|
|setup_intent|50 / 0|0 / 1581|0 / 153|0 / 1|0 / 0|0 / 0|54 / 0|25 / 0|4 / 0|1 / 0|0 / 0|45 / 2|
|payment_intent|175 / 0|0 / 518|1 / 1018|0 / 1|0 / 0|0 / 0|54 / 0|46 / 0|6 / 0|1 / 0|0 / 0|45 / 2|
|terminal_reader|0 / 0|0 / 1822|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|treasury_transaction|0 / 3|0 / 665|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 1|0 / 0|
|core_account|205 / 358|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|56 / 0|1 / 0|1 / 0|0 / 0|0 / 0|
|customer|0 / 6|0 / 569|23 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|account|143 / 188|0 / 92|11 / 0|0 / 0|0 / 0|0 / 0|0 / 0|16 / 0|1 / 0|1 / 0|0 / 0|1 / 1|
|payment_link|0 / 2|0 / 91|0 / 0|0 / 0|0 / 0|0 / 0|15 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|subscription|1 / 2|0 / 74|0 / 0|0 / 0|0 / 19|6 / 1|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|2 / 2|
|checkout_session|7 / 2|0 / 91|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|1 / 0|
|core_account_person|51 / 50|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|0 / 0|
|quote|0 / 2|0 / 91|0 / 0|0 / 0|0 / 0|3 / 1|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|subscription_schedule|14 / 16|0 / 0|0 / 0|0 / 0|0 / 57|0 / 0|0 / 0|0 / 0|1 / 0|1 / 0|0 / 0|0 / 0|
|account_person|43 / 39|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|3 / 0|2 / 0|1 / 0|0 / 0|0 / 0|
|issuing_card|16 / 28|0 / 0|0 / 0|0 / 0|0 / 40|0 / 0|0 / 0|1 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|subscription_item|0 / 0|0 / 0|0 / 0|0 / 0|0 / 60|3 / 1|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|charge|3 / 0|0 / 21|20 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 11|
|treasury_financial_account|0 / 9|0 / 37|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|3 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|customer_payment_method|7 / 2|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 37|
|credit_note|0 / 2|0 / 43|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|invoiceitem|1 / 0|0 / 0|0 / 0|0 / 0|0 / 19|3 / 1|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|refund|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 21|
|tax_transaction|0 / 3|0 / 13|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|
|account_external_account|1 / 1|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|10 / 0|1 / 0|0 / 0|0 / 0|
|mandate|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 11|
|plan|0 / 0|0 / 0|8 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|1 / 0|0 / 0|
|payment_record|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 9|
|identity_verification_session|3 / 3|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 1|
|product_feature|0 / 0|0 / 0|0 / 0|0 / 0|0 / 7|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|issuing_personalization_design|1 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|1 / 0|0 / 0|0 / 0|
|tax_id|2 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|application_fee_refund|0 / 1|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|core_event|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 2|
|core_event_destination|0 / 1|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|customer_balance_transaction|0 / 1|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|event|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 2|
|issuing_dispute|1 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|test_helpers_test_clock|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|1 / 0|0 / 0|0 / 0|
|topup|1 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|transfer_reversal|0 / 1|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|account_capability|0 / 1|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|
|apple_pay_domain|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|balance_settings|0 / 1|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|
|billing_alert|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|billing_credit_grant|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|billing_feedback_option|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|billing_meter|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|billing_meter_event_summary|0 / 1|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|
|billing_portal_configuration|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|climate_order|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|country_spec|0 / 1|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|
|coupon|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|customer_bank_account|0 / 1|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|
|customer_card|0 / 1|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|
|customer_cash_balance_transaction|0 / 1|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|
|entitlements_feature|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|file_link|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|identity_verification_report|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 1|
|invoice_line|0 / 1|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|
|issuing_cardholder|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|payment_method_configuration|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|payment_method_domain|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|payout|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|price|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|product|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|product_catalog_trial_offer|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|promotion_code|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|radar_value_list|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|radar_value_list_item|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|shipping_rate|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|tax_rate|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|tax_registration|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|tax_settings|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 1|
|terminal_configuration|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|terminal_location|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|transfer|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|
|webhook_endpoint|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|0 / 0|1 / 0|0 / 0|0 / 0|

## account: missing leaves by top-level field

|Field|Side|Leaves|Reason|Mechanism|
|-|-|-|-|-|
|`account_token`|req|1|request-only instruction|Keep|
|`bank_account`|req|10|union|Keep|
|`business_profile.minority_owned_business_designation`|req|1|trimmed for lack of proof|ReadOnly|
|`business_profile.monthly_estimated_revenue`|req|2|trimmed for lack of proof|ReadOnly|
|`business_profile.specified_commercial_transactions_act_url`|req|1|trimmed for lack of proof|ReadOnly|
|`business_profile.support_address`|req|6|trimmed for lack of proof|ReadOnly|
|`business_profile.support_email`|req|1|trimmed for lack of proof|ReadOnly|
|`business_profile.support_phone`|req|1|trimmed for lack of proof|ReadOnly|
|`business_profile.support_url`|req|1|trimmed for lack of proof|ReadOnly|
|`capabilities`|resp|69|trimmed for lack of proof|NoResponse|
|`company.address`|req|6|trimmed for lack of proof|Keep|
|`company.address`|resp|6|trimmed for lack of proof|Keep|
|`company.address_kana`|req|7|trimmed for lack of proof|Keep|
|`company.address_kana`|resp|7|trimmed for lack of proof|Keep|
|`company.address_kanji`|req|7|trimmed for lack of proof|Keep|
|`company.address_kanji`|resp|7|trimmed for lack of proof|Keep|
|`company.administrative_address`|req|6|trimmed for lack of proof|Keep|
|`company.administrative_address`|resp|6|trimmed for lack of proof|Keep|
|`company.directors_provided`|req|1|trimmed for lack of proof|Keep|
|`company.directors_provided`|resp|1|trimmed for lack of proof|Keep|
|`company.directorship_declaration`|req|3|trimmed for lack of proof|Keep|
|`company.directorship_declaration`|resp|3|trimmed for lack of proof|Keep|
|`company.executives_provided`|req|1|trimmed for lack of proof|Keep|
|`company.executives_provided`|resp|1|trimmed for lack of proof|Keep|
|`company.export_license_id`|req|1|trimmed for lack of proof|Keep|
|`company.export_license_id`|resp|1|trimmed for lack of proof|Keep|
|`company.export_purpose_code`|req|1|trimmed for lack of proof|Keep|
|`company.export_purpose_code`|resp|1|trimmed for lack of proof|Keep|
|`company.name`|resp|1|trimmed for lack of proof|NoResponse|
|`company.name_kana`|req|1|trimmed for lack of proof|Keep|
|`company.name_kana`|resp|1|trimmed for lack of proof|Keep|
|`company.name_kanji`|req|1|trimmed for lack of proof|Keep|
|`company.name_kanji`|resp|1|trimmed for lack of proof|Keep|
|`company.owners_provided`|req|1|trimmed for lack of proof|Keep|
|`company.owners_provided`|resp|1|trimmed for lack of proof|Keep|
|`company.ownership_declaration`|req|3|trimmed for lack of proof|Keep|
|`company.ownership_declaration`|resp|3|trimmed for lack of proof|Keep|
|`company.ownership_exemption_reason`|req|1|trimmed for lack of proof|Keep|
|`company.ownership_exemption_reason`|resp|1|trimmed for lack of proof|Keep|
|`company.phone`|req|1|trimmed for lack of proof|Keep|
|`company.phone`|resp|1|trimmed for lack of proof|Keep|
|`company.principal_place_of_business`|req|6|trimmed for lack of proof|Keep|
|`company.principal_place_of_business`|resp|6|trimmed for lack of proof|Keep|
|`company.registration_date`|req|3|trimmed for lack of proof|Keep|
|`company.registration_date`|resp|3|trimmed for lack of proof|Keep|
|`company.registration_number`|req|1|write-only value|Keep|
|`company.representative_declaration`|req|3|trimmed for lack of proof|Keep|
|`company.representative_declaration`|resp|3|trimmed for lack of proof|Keep|
|`company.structure`|req|1|trimmed for lack of proof|Keep|
|`company.structure`|resp|1|trimmed for lack of proof|Keep|
|`company.tax_id`|req|1|write-only value|Keep|
|`company.tax_id_provided`|resp|1|trimmed for lack of proof|Keep|
|`company.tax_id_registrar`|req|1|trimmed for lack of proof|Keep|
|`company.tax_id_registrar`|resp|1|trimmed for lack of proof|Keep|
|`company.vat_id`|req|1|write-only value|Keep|
|`company.vat_id_provided`|resp|1|trimmed for lack of proof|Keep|
|`company.verification`|req|2|trimmed for lack of proof|Keep|
|`company.verification`|resp|4|trimmed for lack of proof|Keep|
|`default_currency`|req|1|trimmed for lack of proof|ReadOnly|
|`documents`|req|11|write-only value|Keep|
|`expand`|req|1|request control|Keep|
|`external_accounts`|resp|48|embedded copy of another object|Keep|
|`external_accounts`|resp|3|trimmed for lack of proof|Keep|
|`groups`|req|1|trimmed for lack of proof|Keep|
|`groups`|resp|1|trimmed for lack of proof|Keep|
|`individual.account`|resp|1|embedded copy of another object|Keep|
|`individual.additional_tos_acceptances`|resp|3|embedded copy of another object|Keep|
|`individual.address`|req|2|trimmed for lack of proof|Keep|
|`individual.address`|resp|4|trimmed for lack of proof|NoResponse|
|`individual.address`|resp|2|trimmed for lack of proof|Keep|
|`individual.address_kana`|req|7|trimmed for lack of proof|Keep|
|`individual.address_kana`|resp|7|trimmed for lack of proof|Keep|
|`individual.address_kanji`|req|7|trimmed for lack of proof|Keep|
|`individual.address_kanji`|resp|7|trimmed for lack of proof|Keep|
|`individual.created`|resp|1|embedded copy of another object|Keep|
|`individual.dob`|resp|3|trimmed for lack of proof|NoResponse|
|`individual.email`|resp|1|trimmed for lack of proof|NoResponse|
|`individual.first_name`|resp|1|trimmed for lack of proof|NoResponse|
|`individual.first_name_kana`|req|1|trimmed for lack of proof|Keep|
|`individual.first_name_kana`|resp|1|trimmed for lack of proof|Keep|
|`individual.first_name_kanji`|req|1|trimmed for lack of proof|Keep|
|`individual.first_name_kanji`|resp|1|trimmed for lack of proof|Keep|
|`individual.full_name_aliases`|req|1|trimmed for lack of proof|Keep|
|`individual.full_name_aliases`|resp|1|trimmed for lack of proof|Keep|
|`individual.future_requirements`|resp|9|embedded copy of another object|Keep|
|`individual.gender`|req|1|trimmed for lack of proof|Keep|
|`individual.gender`|resp|1|trimmed for lack of proof|Keep|
|`individual.id`|resp|1|embedded copy of another object|Keep|
|`individual.id_number_provided`|resp|1|embedded copy of another object|Keep|
|`individual.id_number_secondary`|req|1|write-only value|Keep|
|`individual.id_number_secondary_provided`|resp|1|embedded copy of another object|Keep|
|`individual.last_name`|resp|1|trimmed for lack of proof|NoResponse|
|`individual.last_name_kana`|req|1|trimmed for lack of proof|Keep|
|`individual.last_name_kana`|resp|1|trimmed for lack of proof|Keep|
|`individual.last_name_kanji`|req|1|trimmed for lack of proof|Keep|
|`individual.last_name_kanji`|resp|1|trimmed for lack of proof|Keep|
|`individual.maiden_name`|req|1|trimmed for lack of proof|Keep|
|`individual.maiden_name`|resp|1|trimmed for lack of proof|Keep|
|`individual.metadata`|req|1|trimmed for lack of proof|Keep|
|`individual.metadata`|resp|1|trimmed for lack of proof|Keep|
|`individual.nationality`|resp|1|embedded copy of another object|Keep|
|`individual.object`|resp|1|embedded copy of another object|Keep|
|`individual.phone`|resp|1|trimmed for lack of proof|NoResponse|
|`individual.political_exposure`|req|1|trimmed for lack of proof|Keep|
|`individual.political_exposure`|resp|1|trimmed for lack of proof|Keep|
|`individual.registered_address`|req|6|trimmed for lack of proof|Keep|
|`individual.registered_address`|resp|6|trimmed for lack of proof|Keep|
|`individual.relationship`|req|5|trimmed for lack of proof|Keep|
|`individual.relationship`|resp|5|trimmed for lack of proof|Keep|
|`individual.relationship`|resp|3|embedded copy of another object|Keep|
|`individual.requirements`|resp|9|embedded copy of another object|Keep|
|`individual.ssn_last_4_provided`|resp|1|embedded copy of another object|Keep|
|`individual.us_cfpb_data`|resp|5|embedded copy of another object|Keep|
|`individual.verification`|req|3|trimmed for lack of proof|Keep|
|`individual.verification`|resp|7|embedded copy of another object|Keep|
|`individual.verification`|resp|3|trimmed for lack of proof|Keep|
|`individual.verification`|resp|1|trimmed for lack of proof|NoResponse|
|`settings.bacs_debit_payments`|req|1|trimmed for lack of proof|ReadOnly|
|`settings.branding`|req|4|trimmed for lack of proof|ReadOnly|
|`settings.card_issuing`|req|1|trimmed for lack of proof|ReadOnly|
|`settings.card_payments`|req|5|trimmed for lack of proof|ReadOnly|
|`settings.invoices`|req|2|trimmed for lack of proof|ReadOnly|
|`settings.payments`|req|3|trimmed for lack of proof|ReadOnly|
|`settings.payouts`|req|1|union|ReadOnly|
|`settings.payouts`|req|7|trimmed for lack of proof|ReadOnly|
|`settings.paypay_payments`|req|5|trimmed for lack of proof|ReadOnly|
|`settings.paypay_payments`|req|1|write-only value|ReadOnly|
|`settings.paypay_payments`|req|1|empty object|ReadOnly|
|`settings.paypay_payments`|resp|1|empty object|generator|
|`settings.sepa_debit_payments`|req|1|trimmed for lack of proof|ReadOnly|
|`settings.treasury`|req|1|trimmed for lack of proof|ReadOnly|
|`tos_acceptance`|req|2|trimmed for lack of proof|Keep|
|`tos_acceptance`|resp|2|trimmed for lack of proof|Keep|
