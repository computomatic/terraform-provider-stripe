// Package subscription declares the stripe_subscription resource and data source.
package subscription

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/generated/subscription"
)

// Declaration is what the spec cannot say about a subscription. Stripe's DELETE cancels it
// at once (suite: "Subscriptions", `cancel`), and a cancelled subscription stays readable
// with the status `canceled`, so destroy deletes. Cancelling twice is a 404, which the
// engine counts as done.
var Declaration = engine.Declaration{
	TypeName:              "stripe_subscription",
	IDPrefix:              "sub_",
	Description:           "A customer's recurring purchase of one or more prices. `terraform destroy` cancels the subscription immediately, because Stripe cannot delete one: billing stops and the cancelled subscription stays in the account. `items` are matched by `price`: an item whose price stays keeps its ID and changes in place, a new price adds an item, a dropped price deletes it, and a reorder changes nothing. A price may appear once. An item added from outside, such as one that `stripe_subscription_item` created, is not part of `items` and is never changed or deleted by this resource. An update that changes `items` also takes `proration_behavior` and `payment_behavior`. An item that sets `price_data` has no price of its own to match by, so it is matched by its `price_data`: changing its quantity, metadata or other settings updates the item in place, and changing its `price_data` replaces the item, which gets a new item ID. Two items may not carry identical `price_data`. Stripe never returns `add_invoice_items`, so the resource keeps it as configured and sends the whole list on any change to it, and Stripe adds each of its items to the next invoice again. With `collection_method = \"send_invoice\"` the customer needs an email address, or Stripe refuses the create.",
	DataSourceDescription: "A customer's recurring purchase of one or more prices.",
	ImportNote:            "Importing a subscription adopts every item it has, so a configuration that lists fewer items deletes the rest on the next apply. An imported subscription holds `trial_end` and `cancel_at` as the times Stripe returns, so a configuration that sets `trial_end` to `now`, or `cancel_at` to one of its strings, plans an update.",
	ImportNoteLevel:       engine.NoteWarning,
	Paths: engine.Paths{
		Create:   "/v1/subscriptions",
		Retrieve: "/v1/subscriptions/{id}",
		Update:   "/v1/subscriptions/{id}",
		Delete:   "/v1/subscriptions/{id}",
		List:     "/v1/subscriptions",
	},
	// Stripe starts an item at quantity 1 and gives it empty metadata, and sets `cancel_at` to
	// the end of the period when `cancel_at_period_end` is true. It fills in `payment_settings`
	// and `trial_settings` when the request leaves them out, even with no trial (suite:
	// "create/invoiced subscription"). Where a create leaves them out, Stripe also fills
	// `billing_mode.flexible` (suite: TestSubscriptionAttributes, probes `enums/1` and
	// `enums/127`, which sends `billing_mode.type` alone), a billing schedule's duration count
	// (`enums/127`), the invoice issuer's account from `on_behalf_of` (`every_field/19`),
	// `save_default_payment_method` (`enums/100`), the end of a trial `trial_period_days` asks for
	// (`minimal/trial`) and the anchor a trial ends on (`enums/1`).
	ServerDefaulted: []string{
		"cancel_at", "items.quantity", "items.metadata", "payment_settings", "trial_settings",
		"billing_mode.flexible", "billing_mode.flexible.proration_discounts",
		"billing_schedules.bill_until.duration.interval_count", "invoice_settings.issuer.account",
		"payment_settings.save_default_payment_method", "trial_end", "trial_settings.end_behavior.billing_cycle_anchor",
		// Stripe refuses an empty ACSS debit `mandate_options`, its `transaction_type` and a
		// customer balance's `bank_transfer` as `cannot be unset` (probes
		// `isolated/acss_debit_mandate_options_emptied`, `isolated/acss_debit_transaction_type_empty`
		// and `isolated/customer_balance_bank_transfer_emptied*`), so removing one keeps the
		// value Stripe holds and the whole send carries it. Stripe takes the whole `acss_debit` or
		// `customer_balance` sent empty, and drops it (`isolated/acss_debit_emptied` and
		// `isolated/customer_balance_emptied`), which is how the whole send removes either.
		"payment_settings.payment_method_options.acss_debit.mandate_options",
		"payment_settings.payment_method_options.acss_debit.mandate_options.transaction_type",
		"payment_settings.payment_method_options.customer_balance.bank_transfer",
	},
	// Stripe sets `cancellation_details.reason` to `cancellation_requested` when
	// `cancel_at_period_end` turns true, and clears it when it turns false (suite: update
	// "cancel_at_period_end"). It sets `cancel_at` to the end of the period then (suite:
	// TestSubscriptionAttributes, probe `update/38`), a time the record's `side_effects`, which
	// holds literals, does not list, so E5 alone would keep the prior value.
	FilledFrom: map[string][]string{
		"cancel_at_period_end": {"cancel_at", "cancellation_details"},
		// Setting `cancel_at` on an update records the cancellation as requested (suite:
		// TestSubscriptionAttributes, probes `enums/128` and `enums/145`, where the record's
		// `side_effects` lists nothing).
		"cancel_at": {"cancellation_details"},
		// Ending a trial moves the billing cycle anchor to the time it ends (suite:
		// TestSubscriptionAttributes, probe `update/24`, `trial_end = "now"`), a time the
		// record's `side_effects` does not list.
		"trial_end": {"billing_cycle_anchor"},
	},
	// Stripe answers `trial_end = "now"` with the time the trial ended (probe `update/24`), and
	// `cancel_at`'s strings with the time it picks (creates `enums/11`, `enums/29` and
	// `every_field/14`, updates `enums/128`, `enums/145` and `update/12`), so the configured value
	// stays in state, as a tax registration's `active_from` does. Stripe sorts the lists of a
	// US bank account's Financial Connections (the record's `normalised` form `sorted`: a create's
	// `permissions`, probe `every_field/40`, and an update's `account_subcategories` and
	// `prefetch`, probe `update/37`), so they stay as configured too, as an invoice's do.
	// Stripe reads the card's `network` back as sent for a customer with a default card that
	// offers it, and null for a customer with no payment method, which takes any network (probes
	// `isolated/card_network_default_card` and `isolated/card_network_no_payment_method*`), so
	// the configured network stays in state.
	PreserveConfig: []string{
		"cancel_at", "trial_end", "payment_settings.payment_method_options.card.network",
		"payment_settings.payment_method_options.us_bank_account.financial_connections.filters.account_subcategories",
		"payment_settings.payment_method_options.us_bank_account.financial_connections.permissions",
		"payment_settings.payment_method_options.us_bank_account.financial_connections.prefetch",
	},
	// Items are matched by price: an item whose price stays keeps its ID and changes in
	// place, a new price adds an item and a dropped one deletes it (suite: "swap item price"
	// names an item by ID).
	MatchBy: map[string]string{"items": "price"},
	// An item that sets `price_data` is the same item while the data stays; other changes
	// to it apply in place.
	MatchData: map[string]string{"items": "price_data"},
	// How to prorate and pay for a change goes with a change to the items, the one kind of
	// update the suite proves them with.
	SendWithUpdate:   []string{"payment_behavior", "proration_behavior"},
	SendWithUpdateOn: []string{"items"},
	// Stripe keeps the members of `invoice_settings` and `payment_settings` an update leaves out
	// (family `v1-billing`, `hashes`; `payment_settings` by probe `siblings/1`), so an update
	// sends only those that changed; `issuer` requires its `type` and goes whole, and so do
	// `payment_settings.payment_method_options`, which the record does not list, as the update
	// probes sent it. A write of Billie's `company_details` and its `registered_address`, of the
	// card's, Pix's and the US bank account's options, of Financial Connections, or of a card's,
	// Pix's or UPI's `mandate_options`, clears what it leaves out (`siblings_cleared`; probes
	// `isolated/hash_*` and `siblings/32`, which clears the card's `network` too), so the whole
	// send keeps those whole, and leaves out a member the configuration removes, which Stripe
	// refuses as `""` (probes `unset/32`, `unset/35` and `unset/36`). ACSS debit's options and
	// PayTo's `mandate_options` keep what a write leaves out (probes `isolated/hash_acss_debit`
	// and `siblings/34`), which the whole send also holds. Stripe refuses a customer balance's
	// options, and their `bank_transfer`, written in part (probes `isolated/hash_customer_balance`
	// and `isolated/hash_customer_balance_bank_transfer`), which the whole send never does,
	// though it sends what the configuration holds of them. `trial_settings.end_behavior` keeps its
	// siblings too (probe `siblings/27`), but `trial_settings` requires it, so it goes whole.
	SendChangedMembers: []string{"invoice_settings", "payment_settings"},
	SendWholeMembers:   []string{"payment_settings.payment_method_options"},
	Answers: map[string]string{
		// suite: TestSubscriptionAttributes, probe `isolated/invoice_item_coupon_expired`
		"add_invoice_items.discounts.coupon": "Stripe takes a coupon that has not expired.",
		// suite: TestSubscriptionAttributes, probe `isolated/invoice_item_discount_of_subscription`
		"add_invoice_items.discounts.discount": "Stripe takes a discount the subscription or its customer already holds.",
		// suite: TestSubscriptionAttributes, probe `isolated/invoice_item_promotion_code_inactive`
		"add_invoice_items.discounts.promotion_code": "Stripe takes an active promotion code.",
		// suite: TestSubscriptionAttributes, probe `isolated/item_coupon_expired`
		"items.discounts.coupon": "Stripe takes a coupon that has not expired.",
		// suite: TestSubscriptionAttributes, probe `isolated/item_promotion_code_inactive`
		"items.discounts.promotion_code": "Stripe takes an active promotion code.",
		// suite: TestSubscriptionAttributes, probe `isolated/hash_customer_balance_bank_transfer`,
		// whose subscription is created with an EU bank transfer and its `country`
		"payment_settings.payment_method_options.customer_balance.bank_transfer.type": "Stripe takes `eu_bank_transfer` beside `eu_bank_transfer.country`, and refused it on an update that sent no `eu_bank_transfer`.",
		// suite: TestSubscriptionAttributes, probe `isolated/hash_customer_balance`, whose
		// subscription is created with a US bank transfer beside its `funding_type`
		"payment_settings.payment_method_options.customer_balance.funding_type": "Stripe takes `bank_transfer` beside `bank_transfer.type`, and refused it on an update that sent no `bank_transfer`.",
		// suite: TestSubscriptionAttributes, probes `isolated/card_network_*`
		"payment_settings.payment_method_options.card.network": "Stripe takes a network the customer's default card offers, and any network for a customer with no payment method, which it then reads back as null; it refused `amex` for a customer whose card offers only `visa`.",
		// suite: TestSubscriptionAttributes, probes `isolated/acss_debit_*`
		"payment_settings.payment_method_options.acss_debit.mandate_options.transaction_type": "Stripe takes it on an update, and refuses it, or `mandate_options`, sent empty as `cannot be unset`, so removing either from the configuration keeps the current value.",
	},
	Defaults: map[string]string{
		// suite: TestSubscriptionAttributes, probe `every_field/19`, which sends `on_behalf_of`
		// beside `transfer_data`; `every_field/44` sends `transfer_data` alone and holds none
		"invoice_settings.issuer.account": "none, or the `on_behalf_of` account where that is set",
		// suite: TestSubscriptionAttributes, probe `enums/1`, which sends `interval` alone
		"pending_invoice_item_interval.interval_count": "`1` once `pending_invoice_item_interval` is set",
	},
	Delete: engine.Delete,
}

// NewResource returns the resource constructor the provider registers.
func NewResource() (func() resource.Resource, error) {
	return engine.NewResource(Declaration, subscription.Table)
}

// NewDataSource returns the data source constructor. Stripe documents no subscription field as unique, so the data source looks up by `id` only.
func NewDataSource() (func() datasource.DataSource, error) {
	return engine.NewDataSource(Declaration, subscription.Table)
}
