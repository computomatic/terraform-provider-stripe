# terraform-provider-stripe

A Terraform and OpenTofu provider for Stripe, built on the plugin framework.

- The README and `docs/` are for the provider's users. Contributor material lives in `CONTRIBUTING.md`, never in either of them.
- `docs/` is generated (`go generate ./...`). Every resource change regenerates and commits its docs along with its example.
- Resources are built wave by wave against a Stripe API fake, per the ADR.
- The governing design is `adrs/1-pending/2026-10-17-provider-architecture/ADR.md`. Follow it for generation, resource mapping, configuration, API version, client, testing and naming.
- Clean room: never copy source from `stripe/terraform-provider-stripe` or `lukasaron/terraform-provider-stripe`, and do not read it while writing ours. Derive our code from Stripe's public API docs and OpenAPI spec (`stripe/openapi`), and observe other providers' schemas, behaviour and docs instead.
- Stripe is only ever called in a sandbox (`sk_test_` keys). Never print or commit a key, and never run anything locally against the CI sandbox.
