# terraform-provider-stripe

Placeholder spike from April 2025. Its code is not a base for the real provider, which restarts from scratch on the current plugin framework.

- The real provider build is gated on a go/no-go decision (G2), which needs a baseline scorecard of the official provider. Add no new resources until the owner agent says so.
- Clean room applies strictly here. Never copy the official or lukasaron providers' source, and do not read it while writing ours. Observe their schemas, behaviour and docs instead.

## Project context

This repo is part of the Stripe Terraform provider project, owned by a durable agent (`computomatic/agents.stripe-provider-owner`). Build order: a black-box Stripe API conformance suite (`stripe-api-conformance`), a fake Stripe API validated by it (`fake-stripe-api`), then the provider (`terraform-provider-stripe`), with a provider-agnostic scorecard (`terraform-provider-scorecard`) to prove it beats the official provider.

- Stripe is only ever called in a sandbox (`sk_test_` keys). Never print or commit a key, and never run anything locally against the CI sandbox.
- Clean room: never copy source from `stripe/terraform-provider-stripe` or `lukasaron/terraform-provider-stripe`. Derive our code from Stripe's public API docs and OpenAPI spec (`stripe/openapi`).
