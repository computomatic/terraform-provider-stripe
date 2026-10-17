# Architecture documentation updates

Prose below lands under `architecture/` when Step 7 of the ADR runs. The repo's `CLAUDE.md` also gains: "`architecture/` is the canonical source for current system architecture" and "`adrs/` contains architectural decision records and their history".

## `architecture/README.md` (new)

~~~~~markdown
# Architecture

The provider maps Stripe objects to Terraform resources through a generic engine, driven by field tables generated from the pinned OpenAPI spec and a short hand-written declaration per resource.

- [provider/mapping.md](provider/mapping.md): how Stripe objects become resources, the provider configuration, the API version and the naming rules.

Decision history is in `adrs/`.
~~~~~

## `architecture/provider/mapping.md` (new)

~~~~~markdown
# Mapping Stripe to Terraform

## Structure

Field tables in `internal/generated/` come from `spec/openapi.json`, pinned in `spec/PIN.json` to API version `2026-09-30.endive`. Each resource has a declaration in `internal/resources/<resource>/` for what the spec cannot state. `internal/engine` builds the schema and the create, read, update, delete and import behaviour from the two. The engine talks to Stripe through `internal/stripeapi`, a client written for this provider.

## Rules

- A field accepted on create and not on update is create-only and forces replacement. Update requests never include it.
- Destroy deletes when Stripe offers a delete, archives when it offers an off switch, and expires time-bound objects. Archived objects are visible as `active = false`, not removed from state.
- A value Stripe returns once (a webhook secret) is stored in state on create and carried forward on every read.
- A field Stripe defaults is Optional and Computed. Metadata updates send per-key changes.
- References are ID strings; the provider never expands.
- Attribute names are Stripe's, verbatim.

## Provider configuration

`api_key` (required, or `STRIPE_API_KEY`), `base_url`, `files_base_url`, `stripe_account`, `max_retries`. No account ID. Pointing `base_url` at the fake also points file uploads there.

## API version

The version constant is generated from the same pin as the tables and sent on every request. An upgrade regenerates the tables; a breaking schema diff is a major release.

## Naming and import

Type names are `stripe_` plus the Stripe path, singular. Import IDs are the Stripe ID, or `parent/child`. Data sources look up by `id` or a documented unique key and require exactly one match.
~~~~~
