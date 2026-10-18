# terraform-provider-stripe

A Terraform and OpenTofu provider for Stripe, built on the plugin framework.

- The README and `docs/` are for the provider's users. Contributor material lives in `CONTRIBUTING.md`, never in either of them.
- `docs/` is generated (`go generate ./...`). Every resource change regenerates and commits its docs along with its example.
- Resources are built against the fake, wave by wave, per the ADR (the gate was lifted on 2026-10-17).
- The governing design is `adrs/1-pending/2026-10-17-provider-architecture/ADR.md`. Follow it for generation, resource mapping, configuration, API version, client, testing and naming.
- Clean room applies strictly here. Never copy the official or lukasaron providers' source, and do not read it while writing ours. Observe their schemas, behaviour and docs instead.

## Project context

This repo is part of the Stripe Terraform provider project, owned by a durable agent (`computomatic/agents.stripe-provider-owner`). Build order: a black-box Stripe API conformance suite (`stripe-api-conformance`), a fake Stripe API validated by it (`fake-stripe-api`), then the provider (`terraform-provider-stripe`), with a provider-agnostic scorecard (`terraform-provider-scorecard`) to prove it beats the official provider.

- Stripe is only ever called in a sandbox (`sk_test_` keys). Never print or commit a key, and never run anything locally against the CI sandbox.
- Clean room: never copy source from `stripe/terraform-provider-stripe` or `lukasaron/terraform-provider-stripe`. Derive our code from Stripe's public API docs and OpenAPI spec (`stripe/openapi`).
