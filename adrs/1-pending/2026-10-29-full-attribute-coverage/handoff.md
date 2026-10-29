# Attribute proof hand-off: what the provider needs from the conformance suite and the fake

For the suite's and the fake's ADR sessions. It lists, per Stripe object, the attributes and shapes the Terraform provider will expose, what the provider needs proven about each, and the hard cases. It needs no provider code: every path is a dotted path into Stripe's own request and response schemas in the pinned spec (`stripe/openapi` tag `v2585`, API version `2026-09-30.endive`), the same spec the suite and the fake pin.

The direction it serves (the owner, 2026-10-29):

> I expect the fake api to behave like the official stripe API, including attribute parity. That implies the conformance suite also has full coverage of these attributes.

> I'd like the fake stripe API to be so complete and faithful that I could potentially license it to customers who need a fake API to test their own projects against.

The provider exposes an attribute as soon as the pinned fake release's `attributes.json` lists it as `verified` (proven against Stripe by an assertion that also ran on the fake, served, and passing the release's fidelity run), leaf by leaf (the provider's ADR, `ADR.md` beside this file, D1 and D6). Its waves order the work; they are not release gates. The attributes it exposes today stay exposed while the suite audits them against this list; one the audit cannot prove yet is proven in its wave. How the suite proves and how the fake serves are for those repositories' own ADRs. This file says what, not how.

## The files

- `handoff-leaves.tsv`: one row per leaf, 11,069 rows. Columns:

|Column|Meaning|
|-|-|
|`wave`|The provider's wave (1 to 6), the order in which the provider needs objects proven|
|`table`|The provider's table name|
|`terraform_type`|The resource or data source type|
|`side`|`request` (a create or update parameter) or `response` (a field of the object)|
|`path`|Dotted path. An array of hashes or a map of hashes adds no segment: `items.price` is the `price` of each element of `items`|
|`shape`|`string`, `enum`, `integer`, `number`, `boolean`, `list of ...`, `map of ...`, `empty hash`, `expandable reference (ID)`, `embedded copy`, `paged list, returned only when expanded`, `recursion: <schema>`; two shapes joined by ` | ` where a union gives the path both|
|`create`, `update`|For request rows: the create, the update or both take it|
|`exposed_today`|The provider exposes it today|
|`rule`|Empty for an ordinary leaf. Otherwise the class that needs a particular proof (below) or the rule that excludes it from the provider|
|`embedded_object`|For an embedded copy, the object it is a copy of|
|`hard_cases`|Heuristic flags, below|
|`breaking_today`|For an attribute exposed today, the change that would break users and needs evidence before the provider's `v0.1.0` (below)|

- `handoff_test.go.txt`: the throwaway script that wrote the TSV and the tables below (copy it with `measure_test.go.txt` into `tools/schemagen` of the provider at commit `0832bd8` and run `HANDOFF_DIR=<dir> go test ./tools/schemagen -run TestHandoff`).
- `measurement.md`: the provider's own measurement, counted differently (every leaf of every embedded copy, no exclusions).

## Totals

|What|Leaves|Exposed by the provider today|New|
|-|-|-|-|
|Request parameters (62 objects with a resource; `expand` and `include` left out)|3,177|1,971|1,206|
|Response fields of the objects themselves (121 objects)|7,180|5,206|1,974|

Collapsed into single rows, because their proof is the proof of another object plus where they appear:

- 82 embedded copies of other objects, holding 9,524 leaves (an invoice's `last_finalization_error.payment_intent`, a Treasury transaction's `flow_details.outbound_payment`).
- 439 response leaves where the request sends an ID and the response returns the object (a subscription item's `price`); the provider holds the ID.
- 14 paged lists returned only when expanded, which the provider leaves to their own resources (a customer's `subscriptions`).

324 paths are unions: a parameter that takes a string or a hash (`bank_account`: a token such as `btok_us_verified`, or the account details), or a response field with several object shapes (a charge's `source`, a payment intent's `payment_method_options.card`).

## What the provider needs proven

For every leaf the provider will expose (every row whose `rule` does not start with `X`, `excluded` or `ID in`):

|#|Class (`rule` column)|What the provider needs to know, for the object, against Stripe|
|-|-|-|
|P1|every request leaf|Stripe accepts a valid value on create, and on update where the update takes it, for an account where the field applies|
|P2|every request leaf|What the response then holds at the same path: the value as sent, a normalised form (which form: a phone number reformatted, a string lower-cased), or nothing|
|P3|every response leaf|It appears with a value of the spec's shape in an observed response; where it can be null, both null and non-null observed where Stripe produces both|
|P4|nested hashes, arrays, maps, unsetting (one sample per family: v1 Connect, v1 payments, v1 billing, v1 Issuing and Treasury, v2)|An update to one member of a nested hash keeps its siblings; an update to an array replaces it; a map key is removed by `""` (v1) or `null` (v2); a nested scalar is unset by `""` (v1) or `null` (v2)|
|P5|after an update|Which fields the request did not name changed anyway (side effects), and which fields Stripe fills from another field (such as a price's `unit_amount_decimal` from `unit_amount`)|
|P6|`write-only value`|Stripe never returns the value; where the response has a `<name>_provided` flag beside it (`company.tax_id` and `company.tax_id_provided`), the flag turns true|
|P7|`request-only instruction`|Accepted with create, or with the change it qualifies (`proration_behavior` with a change of items), never returned, and its effect|
|P8|`inline data for another object`|The object it creates (`price_data` creates a price) and the response path that references it|
|P9|`union member`|Each form accepted (a test token such as `btok_us_verified` and the hash), and each response variant observed with its fields|
|P10|`embedded copy`|Where and when the copy appears (an error hash's `payment_intent` after a failed payment), and that it equals the copied object; the copied object's own proof covers its fields|
|P11|`objects in, IDs out` (`discounts`)|Each request form (`coupon`, `discount`, `promotion_code`) on create and update, what the response returns (discount IDs; discount objects when expanded), and how each returned discount relates to the form sent|
|P12|`hard_cases` has `country:` or `payment-method:`|Accepted for an account in a country where it applies, and the refusal (status, type, code, param) for a US account|
|P13|defaults (next section)|The value Stripe gives every optional argument left unset|
|P14|lists Stripe returns a page at a time on every read (an invoice's and a credit note's `lines`, a subscription's `items`, a transfer's `reversals`, an application fee's `refunds`, a Radar value list's `list_items`, a Financial Connections session's `accounts`)|What the embedded list holds when the object has more elements than one page: which elements, how many, and `has_more`. The provider keeps these lists and documents that they hold the first page|

## Defaults: what the suite must record

The provider documents, for every optional argument, its default and whether Stripe or the provider sets it (provider issue #64). Stripe's defaults come only from the suite's observations:

- **D-1. A minimal create, per creatable object.** Only the required parameters, and the fewest others Stripe insists on. Record the whole response. An optional argument that comes back non-null has that default; one that comes back null has none.
- **D-2. A minimal create per variant that changes defaults.** An account by `type` and by `controller`; a price one-time and recurring; a payment intent per `capture_method`; and the like, where the suite knows of such variants.
- **D-3. Nested optional arguments.** For each optional hash, a create that sets the hash with only its required members, so the defaults of its other members show (`automatic_tax` with only `enabled`).
- **D-4. The rule, where the default depends on something.** When the observed value depends on the account (its country, default currency, business name) or on another argument (an invoice's `currency` follows its customer's), record that, with the observed value. The provider writes "Default (Stripe): the customer's currency, or else the account's default currency (`usd` on a US account)."
- **D-5. Update-only arguments** need no default: unset, the provider sends nothing.

## What the provider needs from the fake

For every leaf the suite proves:

- **F-1.** Accept it where Stripe does, store it, and return it as Stripe does: the same normalised form, the same defaults (D-1 to D-4), the same `<name>_provided` flags, the same side effects (P5), the same embedded copies where Stripe puts them (P10).
- **F-2.** The update semantics the suite proves (P4).
- **F-3.** The same refusals, including by the account's country (P12).
- **F-4.** A leaf the suite has not proven stays `fake_not_implemented`. The provider does not expose it, so it never sends it.
- **F-5.** Each fake release attaches `attributes.json`, naming the suite commit and evidence run it embedded and every leaf's state; the provider exposes only leaves it lists as `verified` (met by A7 of the fake's ADR, `fake-stripe-api`, `adrs/1-pending/2026-10-29-attribute-parity/ADR.md`, fake PR #65).

## The evidence record the provider consumes

The provider generates its schema and docs from a machine-readable record, pinned like the spec: at a suite commit, per object and path, whether the suite proves it and by which test, the observed default (a value, null, or account- or argument-dependent with the rule), the normalised form, side effects, and refusals with the account country. The provider vendors the immutable per-run file `evidence/<suite_ref>/<run_id>.json` from the suite's `results` branch, at the `suite_ref` and `run_id` its pinned fake release's `attributes.json` names, never the commit's current copy `evidence/<sha>.json`, which changes when the same commit passes again. Its defaults and other facts for the docs come from that file. The format is specified by the suite's ADR (`stripe-api-conformance`, `adrs/1-pending/2026-10-29-attribute-coverage/ADR.md`, suite PR #71).

## Hard cases

Flags in `hard_cases` are heuristics over paths and Stripe's descriptions, to be confirmed by the suite's ADR. Counts are leaves of the objects themselves (collapsed copies not counted), a leaf can have several flags:

|Flag|Leaves|What makes it hard|Examples|
|-|-|-|-|
|`payment-method:<type>(<countries>)`|2,111|Options and details of payment methods a US account may not offer|`payment_method_options.konbini`, `payment_method_details.sepa_debit`|
|`hosted-flow`|331|Filled only when a customer acts in a Stripe-hosted page or a redirect. Proven against Stripe only (`"targets": ["stripe"]`) until the fake serves hosted flows, and not exposed by the provider until a fake release verifies them|`next_action`, `collected_information`, `customer_details`, `three_d_secure`|
|`country:<country>`|254|Accepted only for accounts in some countries, or only meaningful there|`company.address_kana` (Japan), `individual.registered_address`|
|`onboarding`|244|Values that appear as verification and onboarding progress|`requirements`, `future_requirements`, `verification`, `tos_acceptance`, `*_declaration`|
|`file`|229|Needs an uploaded file|`documents.*`, `verification.document.front`|
|`failure-state`|63|Appears only after a failure|`last_payment_error`, `failure_code`|

The owner's direction for country-specific fields: the suite proves them first through connected accounts created in those countries from the US platform (Custom accounts take a `country`); the suite's ADR finds what that cannot reach.

### Country-dependent leaves by object

From the `country:` and `payment-method:` flags; the TSV lists each leaf.

- `stripe_account` (87 leaves): country:Canada, country:Canadian, country:Germany, country:India, country:JP, country:Japan, country:Japanese, country:Mexican, country:New Zealand
- `stripe_account_external_account` (2 leaves): country:India, country:Japan
- `stripe_account_person` (38 leaves): country:Canada, country:JP, country:Thailand
- `stripe_account_v2` (12 leaves): country:Japan
- `stripe_balance` (8 leaves): payment-method:fpx(MY)
- `stripe_charge` (164 leaves): country:Australia, country:India, payment-method:acss_debit(CA), payment-method:afterpay_clearpay(AU/GB/US), payment-method:alipay(CN), payment-method:alma(FR), payment-method:au_becs_debit(AU), payment-method:bacs_debit(GB), payment-method:bancontact(BE), payment-method:billie(DE), payment-method:bizum(ES), payment-method:blik(PL), payment-method:boleto(BR), payment-method:eps(AT), payment-method:fpx(MY), payment-method:giropay(DE), payment-method:grabpay(SG/MY), payment-method:ideal(NL), payment-method:interac_present(CA), payment-method:kakao_pay(KR), payment-method:klarna(EU/US), payment-method:konbini(JP), payment-method:kr_card(KR), payment-method:mobilepay(DK/FI), payment-method:multibanco(PT), payment-method:naver_pay(KR), payment-method:nz_bank_account(NZ), payment-method:oxxo(MX), payment-method:p24(PL), payment-method:payco(KR), payment-method:paynow(SG), payment-method:payto(AU), payment-method:pix(BR), payment-method:promptpay(TH), payment-method:revolut_pay(GB/EU), payment-method:samsung_pay(KR), payment-method:satispay(IT), payment-method:sepa_debit(EU), payment-method:sofort(DE), payment-method:swish(SE), payment-method:twint(CH), payment-method:upi(IN), payment-method:wechat_pay(CN)
- `stripe_checkout_session` (184 leaves): country:JP, payment-method:acss_debit(CA), payment-method:afterpay_clearpay(AU/GB/US), payment-method:alipay(CN), payment-method:alma(FR), payment-method:au_becs_debit(AU), payment-method:bacs_debit(GB), payment-method:bancontact(BE), payment-method:billie(DE), payment-method:blik(PL), payment-method:boleto(BR), payment-method:eps(AT), payment-method:eu_bank_transfer(EU), payment-method:fpx(MY), payment-method:giropay(DE), payment-method:grabpay(SG/MY), payment-method:ideal(NL), payment-method:kakao_pay(KR), payment-method:klarna(EU/US), payment-method:konbini(JP), payment-method:kr_card(KR), payment-method:mobilepay(DK/FI), payment-method:multibanco(PT), payment-method:naver_pay(KR), payment-method:oxxo(MX), payment-method:p24(PL), payment-method:pay_by_bank(GB), payment-method:payco(KR), payment-method:paynow(SG), payment-method:payto(AU), payment-method:pix(BR), payment-method:revolut_pay(GB/EU), payment-method:samsung_pay(KR), payment-method:satispay(IT), payment-method:sepa_debit(EU), payment-method:sofort(DE), payment-method:swish(SE), payment-method:twint(CH), payment-method:upi(IN), payment-method:wechat_pay(CN)
- `stripe_customer_payment_method` (62 leaves): country:India, payment-method:acss_debit(CA), payment-method:au_becs_debit(AU), payment-method:bacs_debit(GB), payment-method:bizum(ES), payment-method:blik(PL), payment-method:boleto(BR), payment-method:eps(AT), payment-method:fpx(MY), payment-method:ideal(NL), payment-method:interac_present(CA), payment-method:klarna(EU/US), payment-method:kr_card(KR), payment-method:naver_pay(KR), payment-method:nz_bank_account(NZ), payment-method:p24(PL), payment-method:payto(AU), payment-method:pix(BR), payment-method:sepa_debit(EU), payment-method:sofort(DE), payment-method:upi(IN)
- `stripe_dispute` (2 leaves): payment-method:klarna(EU/US)
- `stripe_mandate` (32 leaves): payment-method:acss_debit(CA), payment-method:au_becs_debit(AU), payment-method:bacs_debit(GB), payment-method:blik(PL), payment-method:payto(AU), payment-method:pix(BR), payment-method:sepa_debit(EU), payment-method:upi(IN)
- `stripe_payment_intent` (961 leaves): country:JP, country:Japan, payment-method:acss_debit(CA), payment-method:afterpay_clearpay(AU/GB/US), payment-method:alipay(CN), payment-method:alma(FR), payment-method:au_becs_debit(AU), payment-method:bacs_debit(GB), payment-method:bancontact(BE), payment-method:billie(DE), payment-method:bizum(ES), payment-method:blik(PL), payment-method:boleto(BR), payment-method:eps(AT), payment-method:eu_bank_transfer(EU), payment-method:fpx(MY), payment-method:giropay(DE), payment-method:grabpay(SG/MY), payment-method:ideal(NL), payment-method:interac_present(CA), payment-method:kakao_pay(KR), payment-method:klarna(EU/US), payment-method:konbini(JP), payment-method:kr_card(KR), payment-method:mb_way(PT), payment-method:mobilepay(DK/FI), payment-method:multibanco(PT), payment-method:naver_pay(KR), payment-method:nz_bank_account(NZ), payment-method:oxxo(MX), payment-method:p24(PL), payment-method:pay_by_bank(GB), payment-method:payco(KR), payment-method:paynow(SG), payment-method:paypay(JP), payment-method:payto(AU), payment-method:pix(BR), payment-method:promptpay(TH), payment-method:revolut_pay(GB/EU), payment-method:samsung_pay(KR), payment-method:satispay(IT), payment-method:sepa_debit(EU), payment-method:sofort(DE), payment-method:swish(SE), payment-method:twint(CH), payment-method:upi(IN), payment-method:wechat_pay(CN), payment-method:zip(AU)
- `stripe_payment_intent_amount_details_line_item` (4 leaves): payment-method:klarna(EU/US)
- `stripe_payment_method_configuration` (220 leaves): payment-method:acss_debit(CA), payment-method:afterpay_clearpay(AU/GB/US), payment-method:alipay(CN), payment-method:alma(FR), payment-method:au_becs_debit(AU), payment-method:bacs_debit(GB), payment-method:bancontact(BE), payment-method:billie(DE), payment-method:bizum(ES), payment-method:blik(PL), payment-method:boleto(BR), payment-method:eps(AT), payment-method:fpx(MY), payment-method:giropay(DE), payment-method:grabpay(SG/MY), payment-method:ideal(NL), payment-method:kakao_pay(KR), payment-method:klarna(EU/US), payment-method:konbini(JP), payment-method:kr_card(KR), payment-method:mb_way(PT), payment-method:mobilepay(DK/FI), payment-method:multibanco(PT), payment-method:naver_pay(KR), payment-method:nz_bank_account(NZ), payment-method:oxxo(MX), payment-method:p24(PL), payment-method:pay_by_bank(GB), payment-method:payco(KR), payment-method:paynow(SG), payment-method:paypay(JP), payment-method:payto(AU), payment-method:pix(BR), payment-method:promptpay(TH), payment-method:revolut_pay(GB/EU), payment-method:samsung_pay(KR), payment-method:satispay(IT), payment-method:sepa_debit(EU), payment-method:sofort(DE), payment-method:swish(SE), payment-method:twint(CH), payment-method:upi(IN), payment-method:wechat_pay(CN), payment-method:zip(AU)
- `stripe_payment_method_domain` (2 leaves): payment-method:klarna(EU/US)
- `stripe_payment_record` (163 leaves): country:Australia, country:India, payment-method:acss_debit(CA), payment-method:afterpay_clearpay(AU/GB/US), payment-method:alipay(CN), payment-method:alma(FR), payment-method:au_becs_debit(AU), payment-method:bacs_debit(GB), payment-method:bancontact(BE), payment-method:billie(DE), payment-method:bizum(ES), payment-method:blik(PL), payment-method:boleto(BR), payment-method:eps(AT), payment-method:fpx(MY), payment-method:giropay(DE), payment-method:grabpay(SG/MY), payment-method:ideal(NL), payment-method:interac_present(CA), payment-method:kakao_pay(KR), payment-method:klarna(EU/US), payment-method:konbini(JP), payment-method:kr_card(KR), payment-method:mobilepay(DK/FI), payment-method:multibanco(PT), payment-method:naver_pay(KR), payment-method:nz_bank_account(NZ), payment-method:oxxo(MX), payment-method:p24(PL), payment-method:payco(KR), payment-method:paynow(SG), payment-method:payto(AU), payment-method:pix(BR), payment-method:promptpay(TH), payment-method:revolut_pay(GB/EU), payment-method:samsung_pay(KR), payment-method:satispay(IT), payment-method:sepa_debit(EU), payment-method:sofort(DE), payment-method:swish(SE), payment-method:twint(CH), payment-method:upi(IN), payment-method:wechat_pay(CN)
- `stripe_refund` (20 leaves): payment-method:blik(PL), payment-method:eu_bank_transfer(EU), payment-method:gb_bank_transfer(GB), payment-method:jp_bank_transfer(JP), payment-method:mb_way(PT), payment-method:multibanco(PT), payment-method:mx_bank_transfer(MX), payment-method:p24(PL), payment-method:swish(SE)
- `stripe_setup_intent` (193 leaves): payment-method:acss_debit(CA), payment-method:afterpay_clearpay(AU/GB/US), payment-method:alipay(CN), payment-method:alma(FR), payment-method:au_becs_debit(AU), payment-method:bacs_debit(GB), payment-method:bancontact(BE), payment-method:billie(DE), payment-method:bizum(ES), payment-method:blik(PL), payment-method:boleto(BR), payment-method:eps(AT), payment-method:fpx(MY), payment-method:giropay(DE), payment-method:grabpay(SG/MY), payment-method:ideal(NL), payment-method:interac_present(CA), payment-method:kakao_pay(KR), payment-method:klarna(EU/US), payment-method:konbini(JP), payment-method:kr_card(KR), payment-method:mb_way(PT), payment-method:mobilepay(DK/FI), payment-method:multibanco(PT), payment-method:naver_pay(KR), payment-method:nz_bank_account(NZ), payment-method:oxxo(MX), payment-method:p24(PL), payment-method:pay_by_bank(GB), payment-method:payco(KR), payment-method:paynow(SG), payment-method:paypay(JP), payment-method:payto(AU), payment-method:pix(BR), payment-method:promptpay(TH), payment-method:revolut_pay(GB/EU), payment-method:samsung_pay(KR), payment-method:satispay(IT), payment-method:sepa_debit(EU), payment-method:sofort(DE), payment-method:swish(SE), payment-method:twint(CH), payment-method:upi(IN), payment-method:wechat_pay(CN), payment-method:zip(AU)
- `stripe_customer_bank_account` (2 leaves): country:India, country:Japan
- `stripe_customer_card` (2 leaves): country:Australia, country:India
- `stripe_customer_cash_balance_transaction` (9 leaves): payment-method:eu_bank_transfer(EU), payment-method:gb_bank_transfer(GB), payment-method:jp_bank_transfer(JP)
- `stripe_invoice` (50 leaves): payment-method:acss_debit(CA), payment-method:bancontact(BE), payment-method:billie(DE), payment-method:blik(PL), payment-method:eu_bank_transfer(EU), payment-method:konbini(JP), payment-method:payto(AU), payment-method:pix(BR), payment-method:sepa_debit(EU), payment-method:upi(IN)
- `stripe_subscription` (55 leaves): payment-method:acss_debit(CA), payment-method:bancontact(BE), payment-method:billie(DE), payment-method:blik(PL), payment-method:eu_bank_transfer(EU), payment-method:konbini(JP), payment-method:payto(AU), payment-method:pix(BR), payment-method:sepa_debit(EU), payment-method:upi(IN)
- `stripe_issuing_authorization` (1 leaves): country:French
- `stripe_issuing_card` (3 leaves): country:EU, country:Europe
- `stripe_issuing_cardholder` (2 leaves): country:EU
- `stripe_issuing_transaction` (1 leaves): country:French
- `stripe_terminal_location` (32 leaves): country:JP
- `stripe_tax_registration` (54 leaves): country:Canada, country:EU

## Embedded copies

Each is one row in the TSV. The suite proves the copied object once, and for each place below where and when the copy appears (P10).

- `stripe_application_fee`: `refunds.data` (fee_refund)
- `stripe_checkout_session_line_item`: `discounts.discount` (discount), `price` (price), `taxes.rate` (tax_rate)
- `stripe_checkout_session`: `line_items.data` (item), `shipping_cost.taxes.rate` (tax_rate), `total_details.breakdown.discounts.discount` (discount), `total_details.breakdown.taxes.rate` (tax_rate)
- `stripe_climate_order`: `delivery_details.supplier` (climate.supplier)
- `stripe_climate_product`: `suppliers` (climate.supplier)
- `stripe_credit_note_line`: `tax_rates` (tax_rate)
- `stripe_credit_note`: `lines.data` (credit_note_line_item), `shipping_cost.taxes.rate` (tax_rate)
- `stripe_customer`: `discount` (discount)
- `stripe_dispute`: `balance_transactions` (balance_transaction)
- `stripe_financial_connections_session`: `accounts.data` (financial_connections.account), `bank_account_token` (token)
- `stripe_invoice`: `last_finalization_error.payment_intent` (payment_intent), `last_finalization_error.payment_method` (payment_method), `last_finalization_error.setup_intent` (setup_intent), `last_finalization_error.source` (bank_account), `lines.data` (line_item), `shipping_cost.taxes.rate` (tax_rate)
- `stripe_issuing_authorization`: `balance_transactions` (balance_transaction), `card` (issuing.card), `transactions` (issuing.transaction)
- `stripe_issuing_dispute`: `balance_transactions` (balance_transaction)
- `stripe_payment_intent`: `amount_details.line_items.data` (payment_intent_amount_details_line_item), `last_payment_error.payment_method` (payment_method), `last_payment_error.setup_intent` (setup_intent), `last_payment_error.source` (bank_account)
- `stripe_payment_link_line_item`: `discounts.discount` (discount), `price` (price), `taxes.rate` (tax_rate)
- `stripe_payment_link`: `line_items.data` (item)
- `stripe_quote_computed_upfront_line_item`: `discounts.discount` (discount), `price` (price), `taxes.rate` (tax_rate)
- `stripe_quote_line_item`: `discounts.discount` (discount), `price` (price), `taxes.rate` (tax_rate)
- `stripe_quote`: `computed.recurring.total_details.breakdown.discounts.discount` (discount), `computed.recurring.total_details.breakdown.taxes.rate` (tax_rate), `computed.upfront.total_details.breakdown.discounts.discount` (discount), `computed.upfront.total_details.breakdown.taxes.rate` (tax_rate), `line_items.data` (item), `total_details.breakdown.discounts.discount` (discount), `total_details.breakdown.taxes.rate` (tax_rate)
- `stripe_radar_value_list`: `list_items.data` (radar.value_list_item)
- `stripe_reporting_report_run`: `result` (file)
- `stripe_setup_intent`: `last_setup_error.payment_intent` (payment_intent), `last_setup_error.payment_method` (payment_method), `last_setup_error.source` (bank_account)
- `stripe_subscription`: `items.data` (subscription_item), `pending_update.discount` (discount), `pending_update.subscription_items` (subscription_item)
- `stripe_terminal_reader`: `action.api_error.payment_intent` (payment_intent), `action.api_error.payment_method` (payment_method), `action.api_error.setup_intent` (setup_intent), `action.api_error.source` (bank_account), `action.collect_payment_method.payment_method` (payment_method)
- `stripe_transfer`: `reversals.data` (transfer_reversal)
- `stripe_treasury_received_credit`: `linked_flows.source_flow_details.credit_reversal` (treasury.credit_reversal), `linked_flows.source_flow_details.outbound_payment` (treasury.outbound_payment), `linked_flows.source_flow_details.outbound_transfer` (treasury.outbound_transfer), `linked_flows.source_flow_details.payout` (payout)
- `stripe_treasury_transaction_entry`: `flow_details.credit_reversal` (treasury.credit_reversal), `flow_details.debit_reversal` (treasury.debit_reversal), `flow_details.inbound_transfer` (treasury.inbound_transfer), `flow_details.issuing_authorization` (issuing.authorization), `flow_details.outbound_payment` (treasury.outbound_payment), `flow_details.outbound_transfer` (treasury.outbound_transfer), `flow_details.received_credit` (treasury.received_credit), `flow_details.received_debit` (treasury.received_debit)
- `stripe_treasury_transaction`: `flow_details.credit_reversal` (treasury.credit_reversal), `flow_details.debit_reversal` (treasury.debit_reversal), `flow_details.inbound_transfer` (treasury.inbound_transfer), `flow_details.issuing_authorization` (issuing.authorization), `flow_details.outbound_payment` (treasury.outbound_payment), `flow_details.outbound_transfer` (treasury.outbound_transfer), `flow_details.received_credit` (treasury.received_credit), `flow_details.received_debit` (treasury.received_debit)

## Excluded from the provider

The provider leaves these out (`ADR.md`, D1), so it needs nothing proven about them; the fake may still serve them for the owner's licensing aim:

- `expand` and `include` (request controls).
- Response hashes with no keys, which only mark a payment method type.
- The 14 paged lists returned only when expanded, such as a customer's `subscriptions`, `sources` and `tax_ids`, a charge's `refunds`, an account's `external_accounts` and a file's `links`, which their own resources and data sources manage and read. They are excluded from the provider's data sources as well as its resources.
- `confirm` on payment and setup intents, where the provider's `status` attribute confirms.
- Where a response repeats the object itself (a payment intent inside its own `last_payment_error`), below the repeat.

## Needed first: evidence for the provider's `v0.1.0`

The provider's breaking changes on attributes it exposes today must land before its `v0.1.0`, and these need evidence first (the `breaking_today` column):

- **`may-become-computed`, 801 argument leaves on 57 objects.** Today an optional argument the user removes is unset; the provider will instead keep Stripe's value unless the suite shows Stripe leaves the argument null on a minimal create (D-1 to D-3) and proves unsetting for its family (P4). The provider's `v0.1.0` waits on D-1 to D-3 and the unsetting proofs (P4) for these 57 objects, because the owner counts behaviour-only changes as breaking.
- **`read-back`, 22 leaves.** The provider will read back `stripe_account`'s `company.name` and its `individual` fields, and a Treasury financial account's `features.*.requested`, which it keeps from the configuration today. It needs P2 for them.
- **`removed-by-X3`, 6 attributes.** A customer's `tax_ids`, a charge's `refunds`, a file's `links`, an invoice's `payments`, a quote's `computed.upfront.line_items` and a report run's `result.links` leave the provider. Nothing is needed from the suite.

Per object:

|Wave|Terraform type|may-become-computed|becomes-sensitive|read-back|removed-by-X3|new-required|
|-|-|-|-|-|-|-|
|1|`stripe_account`|11|0|13|0|0|
|2|`stripe_account_external_account`|3|0|0|0|0|
|2|`stripe_account_person`|1|0|0|0|0|
|2|`stripe_application_fee_refund`|1|0|0|0|0|
|2|`stripe_account_v2`|6|0|0|0|0|
|2|`stripe_account_person_v2`|5|0|0|0|0|
|2|`stripe_event_destination_v2`|2|0|0|0|0|
|2|`stripe_transfer`|4|0|0|0|0|
|2|`stripe_transfer_reversal`|1|0|0|0|0|
|3|`stripe_charge`|19|0|0|1|0|
|3|`stripe_checkout_session`|162|0|0|0|0|
|3|`stripe_customer_payment_method`|1|0|0|0|0|
|3|`stripe_payment_intent`|30|0|0|0|0|
|3|`stripe_payment_link`|36|0|0|0|0|
|3|`stripe_payment_method_configuration`|1|0|0|0|0|
|3|`stripe_payout`|4|0|0|0|0|
|3|`stripe_radar_value_list`|1|0|0|0|0|
|3|`stripe_refund`|3|0|0|0|0|
|3|`stripe_setup_intent`|10|0|0|0|0|
|3|`stripe_topup`|4|0|0|0|0|
|4|`stripe_billing_alert`|1|0|0|0|0|
|4|`stripe_billing_credit_grant`|5|0|0|0|0|
|4|`stripe_billing_meter`|1|0|0|0|0|
|4|`stripe_billing_portal_configuration`|10|0|0|0|0|
|4|`stripe_coupon`|8|0|0|0|0|
|4|`stripe_credit_note`|6|0|0|0|0|
|4|`stripe_customer`|27|0|0|1|0|
|4|`stripe_customer_balance_transaction`|2|0|0|0|0|
|4|`stripe_entitlements_feature`|1|0|0|0|0|
|4|`stripe_invoice`|54|0|0|1|0|
|4|`stripe_invoiceitem`|3|0|0|0|0|
|4|`stripe_plan`|6|0|0|0|0|
|4|`stripe_price`|11|0|0|0|0|
|4|`stripe_product`|9|0|0|0|0|
|4|`stripe_product_catalog_trial_offer`|1|0|0|0|0|
|4|`stripe_promotion_code`|8|0|0|0|0|
|4|`stripe_quote`|22|0|0|1|0|
|4|`stripe_shipping_rate`|2|0|0|0|0|
|4|`stripe_subscription`|63|0|0|0|0|
|4|`stripe_subscription_item`|1|0|0|0|0|
|4|`stripe_subscription_schedule`|49|0|0|0|0|
|4|`stripe_tax_id`|1|0|0|0|0|
|4|`stripe_tax_rate`|6|0|0|0|0|
|4|`stripe_test_helpers_test_clock`|1|0|0|0|0|
|5|`stripe_issuing_card`|11|0|0|0|0|
|5|`stripe_issuing_cardholder`|20|0|0|0|0|
|5|`stripe_issuing_dispute`|41|0|0|0|0|
|5|`stripe_issuing_personalization_design`|8|0|0|0|0|
|5|`stripe_terminal_configuration`|75|0|0|0|0|
|5|`stripe_terminal_location`|23|0|0|0|0|
|5|`stripe_terminal_reader`|2|0|0|0|0|
|5|`stripe_treasury_financial_account`|4|0|9|0|0|
|6|`stripe_climate_order`|1|0|0|0|0|
|6|`stripe_file`|0|0|0|1|0|
|6|`stripe_file_link`|2|0|0|0|0|
|6|`stripe_identity_verification_session`|6|0|0|0|0|
|6|`stripe_reporting_report_run`|0|0|0|1|0|
|6|`stripe_tax_registration`|2|0|0|0|0|
|6|`stripe_webhook_endpoint`|3|0|0|0|0|

## Per object, in the provider's wave order

Request and response columns count the object's own leaves, with the leaves the provider does not expose today in parentheses. Hard-case columns count leaves.

|Wave|Terraform type|Stripe object|Operations|Request leaves (new)|Response leaves (new)|Embedded copies|Expand-only lists|Unions|country|payment-method|file|onboarding|hosted-flow|failure-state|
|-|-|-|-|-|-|-|-|-|-|-|-|-|-|-|
|1|`stripe_account`|`account`|POST /v1/accounts; POST /v1/accounts/{account}|280 (172)|330 (229)|0|1|3|87|0|32|102|0|0|
|2|`stripe_account_capability`|`capability`|GET /v1/accounts/{account}/capabilities/{capability}|0 (0)|27 (1)|0|0|0|0|0|0|22|0|0|
|2|`stripe_account_external_account`|`bank_account`|POST /v1/accounts/{account}/external_accounts; POST /v1/accounts/{account}/external_accounts/{id}|20 (12)|28 (1)|0|0|0|2|0|1|12|0|0|
|2|`stripe_account_person`|`person`|POST /v1/accounts/{account}/persons; POST /v1/accounts/{account}/persons/{person}|70 (48)|94 (39)|0|0|0|38|0|13|39|0|0|
|2|`stripe_application_fee`|`application_fee`|GET /v1/application_fees/{id}|0 (0)|18 (0)|1|0|0|0|0|0|0|0|0|
|2|`stripe_application_fee_refund`|`fee_refund`|POST /v1/application_fees/{id}/refunds; POST /v1/application_fees/{fee}/refunds/{id}|2 (0)|7 (1)|0|0|0|0|0|0|0|0|0|
|2|`stripe_apps_install`|`apps.install`|GET /v1/apps/installs/{id}|0 (0)|18 (0)|0|0|0|0|0|0|0|0|0|
|2|`stripe_apps_secret`|`apps.secret`|GET /v1/apps/secrets/find|0 (0)|9 (0)|0|0|0|0|0|0|0|0|0|
|2|`stripe_account_v2`|``|POST /v2/core/accounts; POST /v2/core/accounts/{id}|275 (262)|421 (358)|0|0|0|12|0|74|47|0|0|
|2|`stripe_account_person_v2`|``|POST /v2/core/accounts/{account_id}/persons; POST /v2/core/accounts/{account_id}/persons/{id}|74 (52)|77 (50)|0|0|0|0|0|24|0|0|0|
|2|`stripe_event_v2`|``|GET /v2/core/events/{id}|0 (0)|12 (0)|0|0|0|0|0|0|0|0|0|
|2|`stripe_event_destination_v2`|``|POST /v2/core/event_destinations; POST /v2/core/event_destinations/{id}|14 (0)|24 (1)|0|0|0|0|0|0|0|0|0|
|2|`stripe_country_spec`|`country_spec`|GET /v1/country_specs/{country}|0 (0)|10 (1)|0|0|0|0|0|0|0|0|0|
|2|`stripe_transfer`|`transfer`|POST /v1/transfers; POST /v1/transfers/{transfer}|8 (0)|18 (0)|1|0|0|0|0|0|0|0|0|
|2|`stripe_transfer_reversal`|`transfer_reversal`|POST /v1/transfers/{id}/reversals; POST /v1/transfers/{transfer}/reversals/{id}|4 (0)|9 (1)|0|0|0|0|0|0|0|0|0|
|3|`stripe_apple_pay_domain`|`apple_pay_domain`|POST /v1/apple_pay/domains|1 (0)|4 (0)|0|0|0|0|0|0|0|0|0|
|3|`stripe_balance`|`balance`|GET /v1/balance|0 (0)|41 (0)|0|0|0|0|8|0|0|0|0|
|3|`stripe_balance_settings`|`balance_settings`|GET /v1/balance_settings|0 (0)|13 (1)|0|0|0|0|0|0|0|0|0|
|3|`stripe_balance_transaction`|`balance_transaction`|GET /v1/balance_transactions/{id}|0 (0)|19 (0)|0|0|0|0|0|0|0|0|0|
|3|`stripe_charge`|`charge`|POST /v1/charges; POST /v1/charges/{charge}|49 (23)|385 (0)|0|1|2|3|161|0|0|10|2|
|3|`stripe_checkout_session`|`checkout.session`|POST /v1/checkout/sessions; POST /v1/checkout/sessions/{session}|325 (8)|292 (2)|4|0|0|4|180|12|0|32|0|
|3|`stripe_checkout_session_line_item`|`item`|GET /v1/checkout/sessions/{session}/line_items (element)|0 (0)|16 (0)|3|0|0|0|0|0|0|0|0|
|3|`stripe_customer_payment_method`|`payment_method`|POST /v1/payment_methods/{payment_method}/attach; POST /v1/payment_methods/{payment_method}|19 (7)|200 (2)|0|0|0|3|59|2|0|0|0|
|3|`stripe_dispute`|`dispute`|GET /v1/disputes/{dispute}|0 (0)|85 (0)|1|0|0|0|2|9|0|0|0|
|3|`stripe_mandate`|`mandate`|GET /v1/mandates/{mandate}|0 (0)|51 (0)|0|0|0|0|32|0|0|4|0|
|3|`stripe_payment_intent`|`payment_intent`|POST /v1/payment_intents; POST /v1/payment_intents/{intent}|366 (327)|1271 (1014)|4|0|76|5|956|0|0|201|12|
|3|`stripe_payment_intent_amount_details_line_item`|`payment_intent_amount_details_line_item`|GET /v1/payment_intents/{intent}/amount_details_line_items (element)|0 (0)|17 (0)|0|0|0|0|4|0|0|0|0|
|3|`stripe_payment_link`|`payment_link`|POST /v1/payment_links; POST /v1/payment_links/{payment_link}|104 (15)|92 (2)|1|0|0|0|0|0|0|2|0|
|3|`stripe_payment_link_line_item`|`item`|GET /v1/payment_links/{payment_link}/line_items (element)|0 (0)|16 (0)|3|0|0|0|0|0|0|0|0|
|3|`stripe_payment_method_configuration`|`payment_method_configuration`|POST /v1/payment_method_configurations; POST /v1/payment_method_configurations/{configuration}|65 (0)|247 (0)|0|0|0|0|220|0|0|0|0|
|3|`stripe_payment_method_domain`|`payment_method_domain`|POST /v1/payment_method_domains; POST /v1/payment_method_domains/{payment_method_domain}|2 (0)|17 (0)|0|0|0|0|2|0|0|0|0|
|3|`stripe_payment_record`|`payment_record`|GET /v1/payment_records/{id}|0 (0)|326 (0)|0|0|0|3|160|0|0|11|0|
|3|`stripe_payout`|`payout`|POST /v1/payouts; POST /v1/payouts/{payout}|9 (0)|27 (0)|0|0|0|0|0|0|0|0|2|
|3|`stripe_radar_early_fraud_warning`|`radar.early_fraud_warning`|GET /v1/radar/early_fraud_warnings/{early_fraud_warning}|0 (0)|7 (0)|0|0|0|0|0|0|0|0|0|
|3|`stripe_radar_value_list`|`radar.value_list`|POST /v1/radar/value_lists; POST /v1/radar/value_lists/{value_list}|4 (0)|11 (0)|1|0|0|0|0|0|0|0|0|
|3|`stripe_radar_value_list_item`|`radar.value_list_item`|POST /v1/radar/value_list_items|2 (0)|6 (0)|0|0|0|0|0|0|0|0|0|
|3|`stripe_refund`|`refund`|POST /v1/refunds; POST /v1/refunds/{refund}|11 (0)|60 (0)|0|0|0|0|20|0|0|6|1|
|3|`stripe_review`|`review`|GET /v1/reviews/{review}|0 (0)|20 (0)|0|0|0|0|0|0|0|0|0|
|3|`stripe_setup_intent`|`setup_intent`|POST /v1/setup_intents; POST /v1/setup_intents/{intent}|192 (178)|208 (149)|3|1|75|0|193|0|0|34|12|
|3|`stripe_topup`|`topup`|POST /v1/topups; POST /v1/topups/{topup}|9 (1)|17 (0)|0|0|0|0|0|0|0|0|2|
|4|`stripe_billing_alert`|`billing.alert`|POST /v1/billing/alerts|7 (0)|10 (0)|0|0|0|0|0|0|0|0|0|
|4|`stripe_billing_credit_balance_transaction`|`billing.credit_balance_transaction`|GET /v1/billing/credit_balance_transactions/{id}|0 (0)|19 (0)|0|0|0|0|0|0|0|0|0|
|4|`stripe_billing_credit_grant`|`billing.credit_grant`|POST /v1/billing/credit_grants; POST /v1/billing/credit_grants/{id}|13 (0)|19 (0)|0|0|0|0|0|0|0|0|0|
|4|`stripe_billing_feedback_option`|`billing.feedback_option`|POST /v1/billing/feedback_options; POST /v1/billing/feedback_options/{id}|1 (0)|5 (0)|0|0|0|0|0|0|0|0|0|
|4|`stripe_billing_meter`|`billing.meter`|POST /v1/billing/meters; POST /v1/billing/meters/{id}|7 (0)|13 (0)|0|0|0|0|0|0|0|0|0|
|4|`stripe_billing_meter_event_summary`|`billing.meter_event_summary`|GET /v1/billing/meters/{id}/event_summaries (element)|0 (0)|6 (1)|0|0|0|0|0|0|0|0|0|
|4|`stripe_billing_portal_configuration`|`billing_portal.configuration`|POST /v1/billing_portal/configurations; POST /v1/billing_portal/configurations/{configuration}|30 (0)|37 (0)|0|0|0|0|0|0|0|0|0|
|4|`stripe_coupon`|`coupon`|POST /v1/coupons; POST /v1/coupons/{coupon}|12 (0)|16 (0)|0|0|0|0|0|0|0|0|0|
|4|`stripe_credit_note`|`credit_note`|POST /v1/credit_notes; POST /v1/credit_notes/{id}|28 (0)|54 (2)|2|0|0|0|0|0|0|0|0|
|4|`stripe_credit_note_line`|`credit_note_line_item`|GET /v1/credit_notes/{credit_note}/lines (element)|0 (0)|23 (0)|1|0|0|0|0|0|0|0|0|
|4|`stripe_customer`|`customer`|POST /v1/customers; POST /v1/customers/{customer}|69 (23)|56 (0)|1|3|3|0|0|0|0|0|0|
|4|`stripe_customer_balance_transaction`|`customer_balance_transaction`|POST /v1/customers/{customer}/balance_transactions; POST /v1/customers/{customer}/balance_transactions/{transaction}|4 (0)|14 (1)|0|0|0|0|0|0|0|0|0|
|4|`stripe_customer_bank_account`|`bank_account`|GET /v1/customers/{customer}/bank_accounts/{id}|0 (0)|28 (1)|0|0|0|2|0|0|12|0|0|
|4|`stripe_customer_card`|`card`|GET /v1/customers/{customer}/cards/{id}|0 (0)|30 (1)|0|0|0|2|0|0|0|0|0|
|4|`stripe_customer_cash_balance_transaction`|`customer_cash_balance_transaction`|GET /v1/customers/{customer}/cash_balance_transactions/{transaction}|0 (0)|28 (1)|0|0|0|0|9|0|0|0|0|
|4|`stripe_entitlements_active_entitlement`|`entitlements.active_entitlement`|GET /v1/entitlements/active_entitlements/{id}|0 (0)|4 (0)|0|0|0|0|0|0|0|0|0|
|4|`stripe_entitlements_feature`|`entitlements.feature`|POST /v1/entitlements/features; POST /v1/entitlements/features/{id}|4 (0)|6 (0)|0|0|0|0|0|0|0|0|0|
|4|`stripe_invoice`|`invoice`|POST /v1/invoices; POST /v1/invoices/{invoice}|101 (6)|182 (13)|6|2|75|0|50|0|0|0|12|
|4|`stripe_invoice_line`|`line_item`|GET /v1/invoices/{invoice}/lines (element)|0 (0)|43 (1)|0|0|0|0|0|0|0|0|0|
|4|`stripe_invoice_payment`|`invoice_payment`|GET /v1/invoice_payments/{invoice_payment}|0 (0)|15 (0)|0|0|0|0|0|0|0|0|0|
|4|`stripe_invoice_rendering_template`|`invoice_rendering_template`|GET /v1/invoice_rendering_templates/{template}|0 (0)|7 (0)|0|0|0|0|0|0|0|0|0|
|4|`stripe_invoiceitem`|`invoiceitem`|POST /v1/invoiceitems; POST /v1/invoiceitems/{invoiceitem}|27 (4)|34 (1)|0|0|0|0|0|0|0|0|0|
|4|`stripe_plan`|`plan`|POST /v1/plans; POST /v1/plans/{plan}|31 (9)|24 (0)|0|0|2|0|0|0|0|0|0|
|4|`stripe_price`|`price`|POST /v1/prices; POST /v1/prices/{price}|48 (0)|40 (0)|0|0|2|0|0|0|0|0|0|
|4|`stripe_product`|`product`|POST /v1/products; POST /v1/products/{id}|42 (0)|22 (0)|0|0|1|0|0|0|0|0|0|
|4|`stripe_product_catalog_trial_offer`|`product_catalog.trial_offer`|POST /v1/product_catalog/trial_offers; POST /v1/product_catalog/trial_offers/{id}|6 (0)|9 (0)|0|0|0|0|0|0|0|0|0|
|4|`stripe_product_feature`|`product_feature`|POST /v1/products/{product}/features|1 (0)|2 (0)|0|0|0|0|0|0|0|0|0|
|4|`stripe_promotion_code`|`promotion_code`|POST /v1/promotion_codes; POST /v1/promotion_codes/{promotion_code}|13 (0)|17 (0)|0|0|0|0|0|0|0|0|0|
|4|`stripe_quote`|`quote`|POST /v1/quotes; POST /v1/quotes/{quote}|51 (3)|82 (3)|7|1|1|0|0|0|0|0|0|
|4|`stripe_quote_computed_upfront_line_item`|`item`|GET /v1/quotes/{quote}/computed_upfront_line_items (element)|0 (0)|16 (0)|3|0|0|0|0|0|0|0|0|
|4|`stripe_quote_line_item`|`item`|GET /v1/quotes/{quote}/line_items (element)|0 (0)|16 (0)|3|0|0|0|0|0|0|0|0|
|4|`stripe_shipping_rate`|`shipping_rate`|POST /v1/shipping_rates; POST /v1/shipping_rates/{shipping_rate_token}|14 (0)|17 (0)|0|0|0|0|0|0|0|0|0|
|4|`stripe_subscription`|`subscription`|POST /v1/subscriptions; POST /v1/subscriptions/{subscription_exposed_id}|141 (9)|126 (3)|3|0|2|0|55|0|0|1|0|
|4|`stripe_subscription_item`|`subscription_item`|POST /v1/subscription_items; POST /v1/subscription_items/{item}|21 (3)|13 (1)|0|0|0|0|0|0|0|0|0|
|4|`stripe_subscription_schedule`|`subscription_schedule`|POST /v1/subscription_schedules; POST /v1/subscription_schedules/{schedule}|110 (15)|108 (16)|0|0|4|0|0|0|0|0|0|
|4|`stripe_tax_id`|`tax_id`|POST /v1/tax_ids|6 (2)|16 (0)|0|0|0|0|0|0|3|0|0|
|4|`stripe_tax_rate`|`tax_rate`|POST /v1/tax_rates; POST /v1/tax_rates/{tax_rate}|10 (0)|18 (0)|0|0|0|0|0|0|0|0|0|
|4|`stripe_test_helpers_test_clock`|`test_helpers.test_clock`|POST /v1/test_helpers/test_clocks|3 (1)|8 (0)|0|0|0|0|0|0|0|0|0|
|5|`stripe_issuing_authorization`|`issuing.authorization`|GET /v1/issuing/authorizations/{authorization}|0 (0)|84 (0)|3|0|0|1|0|0|0|1|0|
|5|`stripe_issuing_card`|`issuing.card`|POST /v1/issuing/cards; POST /v1/issuing/cards/{card}|38 (17)|64 (28)|0|0|0|3|0|0|0|0|0|
|5|`stripe_issuing_cardholder`|`issuing.cardholder`|POST /v1/issuing/cardholders; POST /v1/issuing/cardholders/{cardholder}|34 (0)|39 (0)|0|0|0|2|0|4|6|0|0|
|5|`stripe_issuing_dispute`|`issuing.dispute`|POST /v1/issuing/disputes; POST /v1/issuing/disputes/{dispute}|45 (1)|52 (0)|1|0|0|0|0|11|0|0|0|
|5|`stripe_issuing_personalization_design`|`issuing.personalization_design`|POST /v1/issuing/personalization_designs; POST /v1/issuing/personalization_designs/{personalization_design}|11 (2)|17 (0)|0|0|0|0|0|3|0|0|0|
|5|`stripe_issuing_physical_bundle`|`issuing.physical_bundle`|GET /v1/issuing/physical_bundles/{physical_bundle}|0 (0)|8 (0)|0|0|0|0|0|1|0|0|0|
|5|`stripe_issuing_token`|`issuing.token`|GET /v1/issuing/tokens/{token}|0 (0)|36 (0)|0|0|0|0|0|0|0|0|0|
|5|`stripe_issuing_transaction`|`issuing.transaction`|GET /v1/issuing/transactions/{transaction}|0 (0)|67 (0)|0|0|0|1|0|0|0|0|0|
|5|`stripe_terminal_configuration`|`terminal.configuration`|POST /v1/terminal/configurations; POST /v1/terminal/configurations/{configuration}|89 (0)|92 (0)|0|0|0|0|0|0|0|0|0|
|5|`stripe_terminal_location`|`terminal.location`|POST /v1/terminal/locations; POST /v1/terminal/locations/{location}|26 (0)|28 (0)|0|0|0|32|0|0|0|0|0|
|5|`stripe_terminal_reader`|`terminal.reader`|POST /v1/terminal/readers; POST /v1/terminal/readers/{reader}|4 (0)|84 (0)|5|1|75|0|0|0|0|0|14|
|5|`stripe_treasury_credit_reversal`|`treasury.credit_reversal`|GET /v1/treasury/credit_reversals/{credit_reversal}|0 (0)|13 (0)|0|0|0|0|0|0|0|0|0|
|5|`stripe_treasury_debit_reversal`|`treasury.debit_reversal`|GET /v1/treasury/debit_reversals/{debit_reversal}|0 (0)|14 (0)|0|0|0|0|0|0|0|0|0|
|5|`stripe_treasury_financial_account`|`treasury.financial_account`|POST /v1/treasury/financial_accounts; POST /v1/treasury/financial_accounts/{financial_account}|17 (3)|71 (46)|0|0|0|0|0|0|0|0|0|
|5|`stripe_treasury_inbound_transfer`|`treasury.inbound_transfer`|GET /v1/treasury/inbound_transfers/{id}|0 (0)|37 (0)|0|0|0|0|0|0|0|0|1|
|5|`stripe_treasury_outbound_payment`|`treasury.outbound_payment`|GET /v1/treasury/outbound_payments/{id}|0 (0)|48 (0)|0|0|0|0|0|0|0|0|0|
|5|`stripe_treasury_outbound_transfer`|`treasury.outbound_transfer`|GET /v1/treasury/outbound_transfers/{outbound_transfer}|0 (0)|45 (0)|0|0|0|0|0|0|0|0|0|
|5|`stripe_treasury_received_credit`|`treasury.received_credit`|GET /v1/treasury/received_credits/{id}|0 (0)|36 (0)|4|0|0|0|0|0|0|0|1|
|5|`stripe_treasury_received_debit`|`treasury.received_debit`|GET /v1/treasury/received_debits/{id}|0 (0)|36 (0)|0|0|0|0|0|0|0|0|1|
|5|`stripe_treasury_transaction`|`treasury.transaction`|GET /v1/treasury/transactions/{id}|0 (0)|16 (0)|8|1|0|0|0|0|0|0|0|
|5|`stripe_treasury_transaction_entry`|`treasury.transaction_entry`|GET /v1/treasury/transaction_entries/{id}|0 (0)|14 (0)|8|0|0|0|0|0|0|0|0|
|6|`stripe_climate_order`|`climate.order`|POST /v1/climate/orders; POST /v1/climate/orders/{order}|6 (0)|28 (0)|1|0|0|0|0|0|0|0|0|
|6|`stripe_climate_product`|`climate.product`|GET /v1/climate/products/{product}|0 (0)|9 (0)|1|0|0|0|0|0|0|0|0|
|6|`stripe_climate_supplier`|`climate.supplier`|GET /v1/climate/suppliers/{supplier}|0 (0)|10 (0)|0|0|0|0|0|0|0|0|0|
|6|`stripe_commerce_product_catalog_import`|``|GET /v2/commerce/product_catalog/imports/{id}|0 (0)|25 (0)|0|0|0|0|0|0|0|0|1|
|6|`stripe_event`|`event`|GET /v1/events/{id}|0 (0)|10 (0)|0|0|0|0|0|0|0|0|0|
|6|`stripe_file`|`file`|GET /v1/files/{file}|0 (0)|9 (0)|0|1|0|0|0|0|0|0|0|
|6|`stripe_file_link`|`file_link`|POST /v1/file_links; POST /v1/file_links/{link}|3 (0)|8 (0)|0|0|1|0|0|2|0|0|0|
|6|`stripe_financial_connections_account`|`financial_connections.account`|GET /v1/financial_connections/accounts/{account}|0 (0)|39 (0)|0|0|0|0|0|0|0|0|0|
|6|`stripe_financial_connections_account_owner`|`financial_connections.account_owner`|GET /v1/financial_connections/accounts/{account}/owners (element)|0 (0)|7 (0)|0|0|0|0|0|0|0|0|0|
|6|`stripe_financial_connections_session`|`financial_connections.session`|GET /v1/financial_connections/sessions/{session}|0 (0)|18 (0)|2|0|0|0|0|0|0|0|0|
|6|`stripe_financial_connections_transaction`|`financial_connections.transaction`|GET /v1/financial_connections/transactions/{transaction}|0 (0)|12 (0)|0|0|0|0|0|0|0|0|0|
|6|`stripe_identity_verification_report`|`identity.verification_report`|GET /v1/identity/verification_reports/{report}|0 (0)|61 (0)|0|0|0|0|0|33|0|0|0|
|6|`stripe_identity_verification_session`|`identity.verification_session`|POST /v1/identity/verification_sessions; POST /v1/identity/verification_sessions/{session}|15 (3)|46 (3)|0|0|0|0|0|8|0|18|2|
|6|`stripe_reporting_report_run`|`reporting.report_run`|GET /v1/reporting/report_runs/{report_run}|0 (0)|15 (0)|1|1|0|0|0|0|0|0|0|
|6|`stripe_reporting_report_type`|`reporting.report_type`|GET /v1/reporting/report_types/{report_type}|0 (0)|8 (0)|0|0|0|0|0|0|0|0|0|
|6|`stripe_tax_code`|`tax_code`|GET /v1/tax_codes/{id}|0 (0)|4 (0)|0|0|0|0|0|0|1|0|0|
|6|`stripe_tax_location`|`tax.location`|GET /v1/tax/locations/{location}|0 (0)|10 (0)|0|0|0|0|0|0|0|0|0|
|6|`stripe_tax_registration`|`tax.registration`|POST /v1/tax/registrations; POST /v1/tax/registrations/{id}|177 (0)|159 (0)|0|0|2|54|0|0|0|0|0|
|6|`stripe_tax_settings`|`tax.settings`|GET /v1/tax/settings|0 (0)|12 (0)|0|0|0|0|0|0|0|0|0|
|6|`stripe_tax_transaction`|`tax.transaction`|GET /v1/tax/transactions/{transaction}|0 (0)|33 (0)|0|1|0|0|0|0|0|11|0|
|6|`stripe_tax_transaction_line_item`|`tax.transaction_line_item`|GET /v1/tax/transactions/{transaction}/line_items (element)|0 (0)|12 (0)|0|0|0|0|0|0|0|0|0|
|6|`stripe_webhook_endpoint`|`webhook_endpoint`|POST /v1/webhook_endpoints; POST /v1/webhook_endpoints/{webhook_endpoint}|7 (0)|11 (0)|0|0|0|0|0|0|0|0|0|
