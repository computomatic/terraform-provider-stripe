## 0.1.0 (Unreleased)

The first release of the Stripe provider for Terraform and OpenTofu, published as `computomatic/stripe`. It is an independent provider, not affiliated with or endorsed by Stripe, Inc.

### What it manages

- 68 resources and 121 data sources.
- Resources cover accounts and Connect (accounts through both Stripe APIs, their people and external accounts), products and pricing, customers and payment methods, billing and subscriptions, payments and checkout, invoicing, tax, Radar, Terminal, Treasury, Issuing, Climate, Identity, files and webhooks.
- Data sources read most of those objects, and the objects Stripe does not let you create, such as balances, balance transactions, events, country specs and tax codes.
- Every request is sent with Stripe API version `2026-09-30.endive`, whatever your account's default version is.
- Each resource and data source has a docs page with its arguments, attributes, an example and how to import it. Notes about one resource or attribute are on its page.

### How it is proven

Every attribute the provider exposes is checked against Stripe by a conformance suite that runs real requests in a sandbox. The acceptance tests run against a fake of Stripe's API that serves only what that suite recorded, so the behaviour the tests exercise is behaviour Stripe has shown.

### Behaviour that applies everywhere

- **Values Stripe never returns** (secrets, tokens, uploaded files and similar) are kept in state from your configuration. Removing one from the configuration stops sending it and leaves the current value on the object.
- **Destroy follows each object's lifecycle in Stripe.** `terraform destroy` deletes an object where Stripe can delete it, and archives it where Stripe has a reversible deactivation. Where Stripe can do neither, destroy fails at plan time, and so does a change that would replace the object. The error shows how to stop managing the object with a `removed` block or `terraform state rm`. Destroy never cancels, voids, expires or detaches anything. Each resource page says what its destroy does.
- **Plan-time refusals.** Where Stripe would refuse a change, the plan fails before anything is sent, for example a change to an argument that cannot change on an object that cannot be replaced.
- **Values Stripe fills in** when you leave an argument out are shown as known after apply in a plan that changes the arguments they depend on. Each optional argument's docs line ends with its default and says what removing it from the configuration does.
- **Country-specific arguments** are accepted in every configuration, because the provider does not know an account's country when it plans. Where an argument does not apply to the account's country, Stripe refuses it when you apply.
- **Import** is supported for resources. Stripe does not return some values, so an imported object has them null; each resource page lists them and the `lifecycle` setting to use.
- **Money movement.** Resources that move money, such as payment intents, refunds, payouts and transfers, are for test data. They refuse to change a live-mode account unless the provider sets `allow_live_money_movement = true`. The Managing Test Data guide explains the workflow.

### Authentication

- Use a restricted key (`rk_test_...` or `rk_live_...`), set in the `STRIPE_API_KEY` environment variable or the `api_key` argument. The Using Restricted Keys guide lists the permission each resource and data source needs. A secret key (`sk_...`) also works, but Stripe advises against it for new uses.
- Set `stripe_account` (or `STRIPE_ACCOUNT`) to act on a connected account with its platform's key.
- Set `stripe_context` (or `STRIPE_CONTEXT`) for organization keys. This project does not test organization keys.
- `base_url`, `files_base_url` and `max_retries` configure the endpoints and the retry count.

### Supported versions

The provider speaks plugin protocol 6 and is tested with Terraform 1.17 and OpenTofu 1.13. `removed` blocks, which stop managing an object without destroying it, need Terraform 1.7 or later or OpenTofu 1.10 or later.

### Known limits

- Some Stripe attributes are not exposed yet, because the conformance suite has not proven them. `internal/generated/coverage.md` lists them.
- Organization keys and `/v2` requests with `stripe_context` are not tested.
- Objects Stripe cannot delete cannot be destroyed by Terraform, and the provider never works around that.
