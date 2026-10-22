# Provider documentation layout, modelled on the AWS provider

## Executive Summary

The provider's pages under `docs/` are rendered by `tfplugindocs` with its default schema layout: one `## Schema` section split into Required, Optional and Read-Only lists, with nested objects in anchored subsections, and an Import section that shows only the `terraform import` command. The owner wants the layout of the AWS provider's pages: `## Argument Reference` and `## Attribute Reference` as separate sections with one bullet per field, and an `## Import` section that leads with an `import` block. This ADR adopts the AWS section order and bullet format, adapted to our provider (nested attributes rather than blocks, Stripe's own wording, replacement and sensitivity markers), and decides to generate it with our own small renderer, `tools/docgen`, that reads the provider's schema in process and replaces `tfplugindocs`. Templates cannot produce the layout, because `tfplugindocs` hands a template one finished Markdown string for the whole schema. Every resource already has working import, and all but one resource's acceptance tests already prove an import block, so the testing change is one missing test and one guard in the layout meta-test.

## Status

Status: Accepted (decided by the owner agent), 2026-10-22, with the six open questions answered as recommended (see "Resolved questions"). The ADR stays in `adrs/1-pending/` until it is built, then moves to `adrs/2-implemented/`.

It extends the accepted architecture (`adrs/1-pending/2026-10-17-provider-architecture/ADR.md`), whose Step 5 wired `tfplugindocs` and the docs check, and whose D7 fixed import IDs and resource identity. It changes the `tfplugindocs` wiring, not the architecture.

Evidence is current as of 2026-10-22: the AWS provider's `main` branch (files read are named below), `terraform-plugin-docs` v0.25.0 (the pinned version, and the latest release), and `terraform-plugin-testing` v1.16.0 (the pinned version, and the latest release).

The decisions most worth scrutiny:

1. **Our own renderer, not custom templates (D3).** This removes `tfplugindocs` from the build and puts a renderer of a few hundred lines in `tools/`.
2. **Nested objects are documented as attributes, not blocks (D1).** AWS says "block" because its resources are SDKv2 and use blocks. Ours are nested attributes (`applies_to = { ... }`), and the pages must show that syntax.
3. **The import example uses `id`, with an identity form second (D4).** AWS leads with `identity`. We lead with `id`, which every supported Terraform and OpenTofu version accepts.
4. **Resource-specific import notes move out of the description into their own declaration field (D4).** Seven resources explain import in their one-paragraph description today.

## Background Context

### How the AWS provider documents a resource

Read from `hashicorp/terraform-provider-aws`, `main`: the contributor guide `docs/end-user-documentation.md`, the resource pages `website/docs/r/sqs_queue.html.markdown`, `lambda_function.html.markdown`, `s3_bucket.html.markdown` and `db_instance.html.markdown`, the data source page `website/docs/d/s3_bucket.html.markdown`, and `website/docs/index.html.markdown`. The pages are hand-written, not generated.

**Resource page, in order** (`sqs_queue`, `lambda_function`):

1. Front matter: `subcategory`, `layout: "aws"`, `page_title: "AWS: aws_sqs_queue"`, `description`.
2. `# Resource: aws_sqs_queue` and a lead paragraph. Callouts follow it when needed: `!> AWS will hang indefinitely ...` (sqs_queue, line 13), `~> **Note:** All arguments including the username and password will be stored in the raw state as plain-text.` (db_instance), `-> **Note:** Write-Only argument \`password_wo\` is available ...`. The guide defines three levels: `->` informational, `~>` warning, `!>` caution, each written `(->|~>|!>) **Note:**`.
3. `## Example Usage`, with `###` subsections for variants ("FIFO queue", "Dead-letter queue"). Code fences are `terraform`, never `hcl`. The guide: "Generally the resource instance name should simply be `example`", "Examples do not need to include every argument. A basic example should use the same configuration as the resource's basic acceptance test", and examples are functional and carry no `terraform` or `provider` block.
4. `## Argument Reference`. Two forms of byline. `sqs_queue` uses "This resource supports the following arguments:" and one list. `lambda_function` (lines 652 to 659) uses "The following arguments are required:" with its list, then "The following arguments are optional:" with its list. The guide says the order is identity arguments, required sorted alphabetically, optional sorted alphabetically.
5. One argument line: `` * `delay_seconds` - (Optional) Time in seconds that the delivery of all messages in the queue will be delayed. An integer from 0 to 900 (15 minutes). The default for this attribute is 0 seconds. `` That is a bullet, the name in backticks, " - ", `(Required)` or `(Optional)`, then the description. Constraints are stated in the description: "Valid values are `messageGroup` and `queue` (default).", "Conflicts with `name_prefix`.", "One of `filename`, `image_uri`, or `s3_bucket` must be specified."
6. Nested blocks: the argument line ends "[See below](#environment-block)." (lambda, line 664 onward) or "See [`ip_rule` Block](#ip_rule-block) below." (guide). After all top-level arguments come `### \`environment\` Block` subsections in alphabetical order, each a bullet list with the same line format. The guide: a nested read-only field is documented inline with `(Read-Only)`.
7. `## Attribute Reference`, with the byline "This resource exports the following attributes in addition to the arguments above:" and a list of `` * `arn` - ARN of the SQS queue. ``, `id` first, then alphabetical, no `(Optional)` marker, no valid values or defaults (guide: "Valid values and default values are not documented"). A resource with none says "This resource exports no additional attributes." Nested exported fields are written as dotted names (`snap_start.optimization_status`, `vpc_config.vpc_id` in lambda) or in a `### \`health\`` subsection.
8. `## Timeouts` (when the resource has them): "[Configuration options](https://developer.hashicorp.com/terraform/language/resources/syntax#operation-timeouts):" then `` * `create` - (Default `10m`) ``.
9. `## Import`, in three parts (`sqs_queue` lines 144 to 180, `lambda_function` lines 796 to 837):
   - "In Terraform v1.12.0 and later, the [`import` block](...) can be used with the `identity` attribute. For example:" then an `import { to = aws_lambda_function.example  identity = { function_name = "example" } }` block, followed by a `resource` block with `### Configuration omitted for brevity ###`.
   - `### Identity Schema` with `#### Required` and `#### Optional` lists in the same line format without the marker.
   - "In Terraform v1.5.0 and later, use an [`import` block](...) to import Lambda Functions using the `function_name`. For example:" with `import { to = ...  id = "example" }`, then "Using `terraform import`, import Lambda Functions using the `function_name`. For example:" and a `console` fence containing `% terraform import aws_lambda_function.example example`.

**Data source page** (`d/s3_bucket`): the same front matter and lead, `## Example Usage`, `## Argument Reference` ("This data source supports the following arguments:", `(Required)` and `(Optional)` lines), `## Attribute Reference` ("This data source exports the following attributes in addition to the arguments above:"). No Import or Timeouts.

**Provider page** (`index.html.markdown`): `# AWS Provider`, `## Example Usage`, `## Authentication and Configuration` with `###` subsections (environment variables, shared files, role assumption), `## Argument Reference` for the provider's own arguments with `###` subsections for its nested blocks.

**Description rules** (guide): start with a verb or noun, never "A ", "An ", "The "; boolean descriptions start "Whether to "; do not spell out common abbreviations; document allowed values (`` Allowed values are: `a`, `b`. ``) and defaults (`` Default value: `ENABLED`. ``) on arguments.

### How our docs are produced today

- `internal/provider/generate.go:7` runs `go tool tfplugindocs generate --provider-dir ../.. --provider-name stripe --rendered-provider-name Stripe` (`terraform-plugin-docs` v0.25.0, `go.mod:88`). `internal/generated/generate.go:5` runs `tools/schemagen` first, which writes the field tables from `spec/openapi.json`.
- `tfplugindocs` starts the built provider, asks the Terraform CLI for `providers schema -json`, and renders each resource and data source. Our only template is `templates/index.md.tmpl`; resource and data source pages use the built-in template. The inputs are `examples/resources/<type>/resource.tf` and `import.sh`, `examples/data-sources/<type>/data-source.tf`, and `examples/provider/provider.tf`.
- `scripts/check-docs.sh` copies `docs/`, runs `go generate ./...` and fails when the tree differs. The `docs` CI job runs it, and it needs a `terraform` or `tofu` binary.
- Today's page (`docs/resources/coupon.md`): front matter, `# stripe_coupon (Resource)`, description, `## Example Usage`, then `## Schema` with `### Optional` and `### Read-Only` lists in the form `` - `amount_off` (Number) Description. `` and `### Nested Schema for \`applies_to\`` sections with `<a id="nestedatt--applies_to"></a>` anchors, then `## Import` with "Import is supported using the following syntax:" and "The [`terraform import` command](...) can be used, for example:" followed by the `import.sh` file.
- Our schema is built in `internal/engine/schema.go`. Descriptions come from Stripe's reference text through the generated tables. The engine appends " Changing it replaces the resource." to an argument that forces replacement (`schema.go:43`; the parent reference at `schema.go:80`). Nested objects are `SingleNestedAttribute`, `ListNestedAttribute` and `MapNestedAttribute`, so the pages say "Attributes", and `timeouts` is a nested attribute (`timeouts = { create = "5m" }`). No attribute is write-only today. `Sensitive` is rendered by `tfplugindocs` as ", Sensitive" in the type.
- A resource's `Description` (`internal/engine/declaration.go:140`) is one paragraph that mixes the summary with destroy behaviour, replacement rules and import caveats.

### What `tfplugindocs` can do (v0.25.0, the latest release)

Read from `terraform-plugin-docs@v0.25.0/internal/provider`:

- **Templates.** `templates/resources/<name>.md.tmpl` per resource, `templates/resources.md.tmpl` as the fallback for all, and the same for `data-sources`. The template data (`ResourceTemplateType`, `template.go:218` onward) is `Name`, `Type`, `Description`, `HasExample`, `ExampleFile`, `ExampleFiles`, `HasImport`, `ImportFile`, `HasImportIDConfig`, `ImportIDConfigFile`, `HasImportIdentityConfig`, `ImportIdentityConfigFile`, `IdentitySchemaMarkdown`, `ProviderName`, `ProviderShortName`, `RenderedProviderName` and `SchemaMarkdown`. Functions: `codefile`, `lower`, `plainmarkdown`, `prefixlines`, `split`, `tffile`, `title`, `trimspace`, `upper`. There is no `replace`, no `contains`, and no access to the schema's structure.
- **`SchemaMarkdown` is one finished string** whose headings are fixed in `schemamd/render.go:186` to `188` (`### Required`, `### Optional`, `### Read-Only`) and `:509` (`### Nested Schema for`). A template can place that string but cannot split it into an Argument Reference and an Attribute Reference, reformat a line, or add `(Required)`.
- **Import files.** `examples/resources/<type>/import.sh` (the command), `import-by-string-id.tf` (an `import` block with `id`) and `import-by-identity.tf` (an `import` block with `identity`, which also makes it render the identity schema; it errors when the resource has no identity). The built-in template (`template.go:323` to `371`) renders, under `## Import`: the identity form, then the `id` form ("In Terraform v1.5.0 and later, the `import` block can be used with the `id` attribute, for example:"), then the command. So the import part alone needs no renderer of our own.
- **Flags.** `--rendered-website-dir`, `--tf-version`, `--ignore-deprecated` and a few others. None changes the schema layout.

### Import today

Every resource has `ImportState` in the engine (`internal/engine/resource.go:506`). It takes the Stripe ID, or `<parent_id>/<child_id>` for a child, and also reads a resource identity whose attributes are `id` and the parent attribute (`resource.go:507` to `522`, `api.go:135`). 36 of the 37 resource test files with an import step also run a second step with `ImportStateKind: resource.ImportBlockWithID` (for example `internal/resources/coupon/resource_test.go:149`); `subscription_schedule` has only the command-style step (`resource_test.go:200` to `208`). No test uses `ImportBlockWithResourceIdentity`. `internal/resources/layout_test.go` requires a test named for `import` and an `import.sh` per resource. CI runs Terraform 1.16.6 and OpenTofu 1.12.3 (`.github/workflows/ci.yml:58`, `:85`). OpenTofu 1.12 added import by resource identity (its 1.12.0 beta announcement, https://opentofu.org/blog/help-us-test-opentofu-1-12-0-beta1/); the final release notes were not read.

Seven pages explain import in their description, in prose: `climate_order`, `identity_verification_session` and `terminal_reader` (a field Stripe does not return after import, and what the next plan does), `subscription` (import adopts every item), `webhook_endpoint` (no secret after import), `customer_payment_method` and `product_feature` (the ID shape).

## Approach Overview

- **Layout (D1).** Resource pages follow the AWS section order: lead, notes, Example Usage, Argument Reference, Attribute Reference, Timeouts, Import. Data source pages and the provider page follow the same order without the sections that do not apply. A table in D1 records each AWS convention as adopted, adapted or not adopted.
- **Line format (D2).** `` * `name` - (Required) Description. `` for arguments, `` * `name` - Description. `` for attributes, with explicit markers for sensitive, write-only and read-only nested fields.
- **Generation (D3).** `tools/docgen`, a Go program run by `go generate`, reads the provider's schema in process and the examples, and writes `docs/`. `tfplugindocs` and `templates/index.md.tmpl` are removed.
- **Import (D4).** For every resource: an `import` block with `id`, an `import` block with `identity` where the CLI supports it, the `terraform import` command, and a resource-specific note taken from a new declaration field.
- **Tests (D5).** The `id` import-block step is required of every resource by the layout meta-test; the identity form is proven on a representative set.
- **Roadmap.** Five steps, each reviewable alone: renderer, switch of resources and data sources, import section and examples, provider page, tests and docs.

## Approach Details

### D1. Page layout and the AWS conventions

Resource page, in order:

~~~~~markdown
---
page_title: "stripe_coupon Resource - Stripe"
subcategory: ""
description: |-
  <description, first paragraph, plain text>
---

# Resource: stripe_coupon

<lead paragraph>

<callouts, if any>

## Example Usage

<resource.tf>

## Argument Reference

The following arguments are required:

* `duration` - (Required) ...

The following arguments are optional:

* `amount_off` - (Optional) ... See [`applies_to`](#applies_to) below.

### `applies_to`

The `applies_to` attribute supports:

* `products` - (Optional) ...

## Attribute Reference

This resource exports the following attributes in addition to the arguments above:

* `created` - ...

## Timeouts

## Import
~~~~~

How each AWS convention is treated:

| AWS convention | Decision | Reason |
| --- | --- | --- |
| Heading `# Resource: <type>` and `# Data Source: <type>` | Adopt | Matches AWS; today's `# stripe_coupon (Resource)` is the `tfplugindocs` form. |
| Front matter without `layout` | Adapt | `layout` and `subcategory` are AWS-site specific. We keep `page_title` and `description`, and an empty `subcategory`, as the Registry expects. |
| `## Argument Reference` and `## Attribute Reference` | Adopt | The owner's request. Arguments are what the user sets: Required and Optional, including Optional and Computed. Attributes are what the resource exports. |
| Required and optional in two lists with the two AWS bylines | Adopt | Our resources have long optional lists (a customer has dozens), so the split helps. A list that would be empty is omitted with its byline. |
| One line per field, `` * `name` - (Required) Description. `` | Adopt | See D2. |
| Order: required, then optional, alphabetical; `id` first among attributes | Adopt, without the identity-arguments-first rule | The AWS rule needs a notion of identity arguments. Ours are the parent reference of a child resource, a required field that sorts normally; adding a rule for it buys little. |
| "This resource exports no additional attributes." | Adopt | Rare here, because `id` and `livemode` are always exported; kept for completeness. |
| Nested objects as `### \`name\` Block` subsections with "[See below]" links | Adapt | Ours are nested attributes, so the subsection is titled `` ### `applies_to` `` and says "The `applies_to` attribute supports:", never "block". A nested object inside a nested object gets its own subsection titled by its dotted path (`` ### `phases.items` ``), ordered depth first, alphabetical within a level. A nested field that is read-only is listed inline in its subsection with `(Read-Only)`, as the AWS guide asks. |
| Exported nested fields written as dotted names (`vpc_config.vpc_id`) | Not adopted | A read-only top-level nested attribute is listed once and linked to its subsection, which lists every field, so the page has one place per field. |
| Description style: starts with a verb or noun, never "A ", "The "; booleans start "Whether to " | Not adopted | Descriptions are Stripe's own reference text, taken from the pinned spec by `tools/schemagen`. Rewording 69 resources' fields would be a hand-maintained layer that drifts on every spec bump, and the D7 rule of the architecture ADR (Stripe's vocabulary, so docs map one to one to Stripe's reference) is better served by the verbatim text. |
| Attribute descriptions omit valid values and defaults | Not adopted | Our descriptions are shared by the argument and the attribute and already carry "Possible values" and "Defaults to" from the spec; stripping them would need a second text per field. |
| Do not spell out common abbreviations | Not applicable | The list is AWS terms. |
| Callouts `->`, `~>`, `!>` with `**Note:**` | Adopt the sigils, drop the bold prefix | The Registry turns a paragraph that starts with a sigil into a coloured callout and adds the bold prefix itself (`->` and `~>` as Note, `!>` as Warning), so writing `**Note:**` would print it twice. A callout is one paragraph, the only form the Registry supports. Use `~>` for behaviour that surprises (destroy archives rather than deletes, import adopts every item), `!>` for data loss, `->` for tips. See D4 for where they come from. |
| `## Timeouts` section with "(Default `10m`)" | Adopt, shorter | Our timeouts are an attribute (`timeouts = { create = "5m" }`), and the section says so with a short example and lists the three keys with their defaults. It replaces the `timeouts` nested subsection. |
| `terraform` code fences, example instance named `example`, no `provider` block | Adopt | The existing examples already use `terraform` fences. A check in `layout_test.go` (D5) can enforce the block rule. The instance name stays as the example author chose (`launch`, `gold_search`), because several examples reference each other; renaming is cosmetic and not in scope. |
| `###` example variants under Example Usage | Adopt | `tfplugindocs` already renders every `resource*.tf` it finds; the renderer does the same, one `###` heading per extra file, titled from a first-line comment `# Title`. |
| `region`, `tags` and `tags_all` conventions | Not applicable | |
| `Identity Schema` subsection | Adapt | See D4. |

Differences specific to this provider, each shown in the argument line (D2):

- **Forces replacement.** The engine's sentence "Changing it replaces the resource." stays at the end of the description and is also the only marker. The AWS provider has no such marker; AzureRM's "Changing this forces a new resource to be created" is the closest convention. Keeping the sentence costs nothing now. A marker in the line, `(Optional, Forces replacement)`, is the alternative raised in the open questions.
- **Sensitive.** `(Optional, Sensitive)`, or `(Sensitive)` on an attribute, because the value is stored in state. AWS has no marker; `tfplugindocs` prints ", Sensitive" and users rely on it.
- **Write-only.** `(Optional, Write-Only)`, with a one-line `->` callout under the section that the value needs Terraform 1.11 or later. None exists; the renderer supports it so that the first one needs no change here.
- **Create-only fields** (an argument Stripe accepts only on create, which the engine keeps in state from the configuration). These are the replacement-forcing arguments above; the description already says so.
- **Stripe vocabulary.** Names, enum values and descriptions stay Stripe's, verbatim, as D7 of the architecture ADR requires.

**Data source page.** `# Data Source: stripe_coupon`, lead, Example Usage, `## Argument Reference` with "This data source supports the following arguments:" and the lookup keys (`id`, or the keys the declaration lists, with "Exactly one must be set." as the lead sentence under the byline), `## Attribute Reference` with "This data source exports the following attributes in addition to the arguments above:" listing every other field. No Timeouts or Import.

**Provider page.** `# Stripe Provider`, lead paragraph with the independence notice, `## Example Usage`, `## Authentication` and `## Stripe API version` (hand-written text kept in the renderer's input, not in a Go string; see D3), and `## Argument Reference` with "This provider supports the following arguments:" and the five provider arguments. The environment variable each falls back to is already in the schema description ("Defaults to the `STRIPE_API_KEY` environment variable.").

### D2. The argument and attribute lines

- Argument: `` * `name` - (Required) Description. `` Markers go inside one pair of parentheses, comma separated, in this order: `Required` or `Optional`, then `Sensitive`, then `Write-Only`, then `Deprecated`. Optional and Computed arguments are `(Optional)`.
- Attribute (read-only, top level): `` * `name` - Description. `` with `(Sensitive)` ahead of the description when it applies. `id` is always first and always present, also when the resource lets the user choose it (coupon): then `id` appears in the arguments as `(Optional)` and again in the attributes as the Stripe ID.
- A nested attribute's line ends with " See [`name`](#name) below." The anchor is an explicit `<a id="name"></a>` before the subsection heading (as `tfplugindocs` does today), because the Registry's generated heading anchors are not guaranteed to be stable for headings that contain code spans.
- The nested type is stated in the subsection byline, not the line: "The `currency_options` attribute is a map. Each value supports:". For a list: "Each element supports:". For a single object: "The `applies_to` attribute supports:".
- Descriptions are written on one line each, as today, with no wrapping.

### D3. How the pages are generated

**Decision: our own renderer, `tools/docgen`.**

Why templates alone do not reach the layout. A `tfplugindocs` template receives the schema only as `SchemaMarkdown`, one string with fixed headings. The AWS layout needs the Argument Reference and Attribute Reference as separate sections, `(Required)` markers on each line, and the dotted-path subsections for nested attributes. With the functions `tfplugindocs` provides (`trimspace`, `split`, `lower`, `upper`, `title`, `prefixlines`, `plainmarkdown`, `tffile`, `codefile`), a template could at most cut the string at the "### Read-Only" heading by splitting on it, and then it would still print the same `(Number)` line format. That is a fragile string surgery on another tool's output, and it breaks silently when a `tfplugindocs` release changes a heading. The alternative of post-processing the generated files is the same surgery done later.

What `tools/docgen` is:

- **Input.** The provider's schema, read in process: build the provider through `providerserver.NewProtocol6(provider.New(...))()` and call `GetProviderSchema`, which returns every resource and data source schema with attributes, nested attributes, `Required`/`Optional`/`Computed`, `Sensitive`, `WriteOnly`, `Deprecated` and descriptions, plus `GetResourceIdentitySchemas` for identity. No Terraform CLI, no build of the provider binary and no network. The conversion to a small internal model (name, kind, flags, description, children) is the only place that knows the protocol types.
- **Other inputs.** `examples/` as today (`resource.tf` and variants, `import.sh`, `import-by-string-id.tf` and `import-by-identity.tf`, `data-source.tf`, `provider.tf`), and the resource's `Description` and notes from its declaration (D4). The provider page's prose (Authentication, API version) moves from `templates/index.md.tmpl` to a Markdown file `docs-src/provider.md` read by the renderer. The `{{ .Description }}` placeholder in that template, which pulls in the API-version sentence from the provider schema, becomes a plain read of the provider schema description.
- **Output.** `docs/index.md`, `docs/resources/<name>.md`, `docs/data-sources/<name>.md`, in the layout of D1. Output is deterministic: sorted names, no timestamps, LF endings, a trailing newline.
- **Wiring.** `internal/provider/generate.go` runs `go run ../../tools/docgen -root ../..` in place of the `tfplugindocs` line. `scripts/check-docs.sh` is unchanged except that it no longer needs a Terraform or OpenTofu binary; the comment about the binary is removed. Removing `tfplugindocs` removes the `tool` line in `go.mod` and its indirect dependencies on `go mod tidy`.
- **Registry compliance.** See D3a.
- **Size.** About 400 lines of Go plus tests: model conversion (80), the line and subsection writers (150), the page assemblers (100), file handling (70). That is small, and golden-file tests (D5) pin the output.

**Costs of this choice.** We own the renderer, so a new schema feature (a new attribute kind) needs a case in it; the converter fails on an unknown kind rather than printing a wrong page. We lose upstream fixes in `tfplugindocs`, but its schema rendering has not been what we need to track. It also moves the docs check off the Terraform CLI, which is a gain in CI speed.

### D3a. Registry compliance

The renderer owns everything the Terraform and OpenTofu registries need from `docs/`. Requirements are from https://developer.hashicorp.com/terraform/registry/providers/docs (read 2026-10-22).

- **Directory layout (current format).** `docs/index.md` (required), `docs/resources/<name>.md` and `docs/data-sources/<name>.md`, with the file name being the type name without the `stripe_` prefix, and the `.md` extension. Optional `docs/guides/<guide>.md`. The renderer writes the first three. It deletes any page under `docs/resources/` or `docs/data-sources/` that it did not write, so a removed resource leaves no stale page. There is no guide today; hand-written guides are not generated, so if one is added it lives in `docs-src/guides/` and the renderer copies it to `docs/guides/` (the folder `docs/` stays wholly generated, and `scripts/check-docs.sh` stays valid). The legacy `website/docs` layout is not used. Functions, ephemeral resources, actions and list resources do not exist in this provider.
- **Front matter.** The Registry treats it as optional, does not render it, and requires `page_title` only for guides. We write it on every page anyway, as `tfplugindocs` does today (current files: `docs/resources/coupon.md` has `page_title: "stripe_coupon Resource - Stripe"`, `subcategory: ""`, and a multi-line `description: |-`; `docs/index.md` has `page_title` and `description` only): `page_title`, `subcategory` (empty), and `description` (the first paragraph of the lead as plain text, via the same Markdown stripping `tfplugindocs` applies). The leading comment `# generated by https://github.com/hashicorp/terraform-plugin-docs` is replaced by `# generated by tools/docgen`. Guides carry the `page_title` their source file gives.
- **Callouts.** One paragraph each, sigil first, no bold prefix (D1).
- **Size limit.** "Each document can contain no more than 500KB of data. Longer documents are truncated, and the Registry displays a note." Truncation is silent in the build, so the renderer must refuse to produce such a page.
  - *Where the pages stand.* Today's largest are `docs/data-sources/checkout_session.md` (145,841 bytes) and `docs/resources/terminal_reader.md` (125,383 bytes, 1,712 lines); the sum of all `docs/` is about 2.06 MB. Line count is not the measure: the limit is bytes, and the longest page is 29% of it.
  - *What keeps every page under it.* The layout adds no content to a page. It drops the type text (`(Number)`, `(Attributes)`) from each line, so a page is no larger than today's and usually a little smaller. Each field appears once on a page: a nested object is one subsection listed under its dotted path and linked from its parent. A data source lists each field once, as an attribute. The only growth is the Import section (about 1 KB with the Identity Schema). The largest page therefore stays near 30% of the limit.
  - *Test.* A unit test renders every resource, data source and the index from the real provider schema and fails when any page exceeds 250,000 bytes, half the Registry limit, naming the page. The bound is deliberately below 500KB (whether read as 500,000 or 512,000 bytes) so growth is caught long before truncation. The same check runs over the golden fixture. A spec bump that doubles `checkout_session` still passes; one that nearly doubles it again fails and forces a decision (split the page or omit a subtree, see below) before the Registry truncates anything.
  - *If a page ever needs to be smaller.* The renderer elides nothing on its own. The remedy is a declaration-level setting that omits a nested attribute's subtree from the docs with a one-line pointer to Stripe's API reference; it is not built now and would be a new ADR decision.

### D4. The Import section

For every resource, in this order:

~~~~~markdown
## Import

In Terraform v1.5.0 and later, and in OpenTofu, use an [`import` block](https://developer.hashicorp.com/terraform/language/import) to import a coupon using its ID. For example:

```terraform
import {
  to = stripe_coupon.example
  id = "launch_offer"
}
```

In Terraform v1.12.0 and later, and in OpenTofu v1.12.0 and later, the `import` block can use the `identity` attribute instead. For example:

```terraform
import {
  to = stripe_coupon.example
  identity = {
    id = "launch_offer"
  }
}
```

### Identity Schema

#### Required

* `id` (String) The Stripe ID of the coupon.

Using `terraform import`, import a coupon using its ID. For example:

```console
% terraform import stripe_coupon.example launch_offer
```

-> <resource-specific note, when there is one: one paragraph, no bold prefix>
~~~~~

- **`id` form first.** AWS leads with `identity`. We lead with `id` because it works on Terraform 1.5 and later and on every OpenTofu release that has import blocks, while the identity form needs 1.12 on either CLI. The brief asks for "an `import` block example first, then the `terraform import` command"; this keeps both and puts the identity form between them as an addition, as AWS does after its block.
- **Child resources** show both IDs. The `id` form uses the slash form (`id = "prod_123/prodft_456"`), the identity form shows both attributes (`product = "prod_123"`, `id = "prodft_456"`), and the Identity Schema lists the parent attribute as Required first. **Singletons** use the constant `default` (D7 of the architecture ADR); no resource page has one yet.
- **Source of the examples.** The renderer writes the three code samples from the declaration (type name, parent, singleton) and a sample ID: the `import.sh` file's ID argument, which every resource has today. That removes the need for new `import-by-string-id.tf` and `import-by-identity.tf` files per resource: the `import.sh` stays the single place a resource author states an example ID, and a layout test (D5) checks that the command in `import.sh` has the form `terraform import <type>.<name> <id>`. The comment line that `import.sh` carries for child resources ("The ID is the product ID and the product feature ID, separated by a slash.") becomes the lead sentence of the Import section.
- **Resource-specific notes.** A new `Declaration.ImportNote` field (Markdown, optional) holds what the description says about import today: `climate_order` and `identity_verification_session` (Stripe does not return `amount` or `provided_details`, so an imported object has none in state and configuring one plans a replacement), `terminal_reader` (`registration_code`, with the `ignore_changes` remedy), `webhook_endpoint` (no `secret`), `subscription` (import adopts every item and a shorter list deletes the rest), and the ID shape sentences of `customer_payment_method` and `product_feature` (covered by the generated child-resource text, so those two notes are deleted rather than moved). The renderer prints it as a `~>` callout when it describes a consequence (`subscription`, `terminal_reader`, `climate_order`, `identity_verification_session`) and as `->` otherwise (`webhook_endpoint`); the level is a second field, `ImportNoteLevel`, defaulting to `->`. Moving the text means editing seven descriptions and their regenerated pages in the same change.
- **A lead that is still too long.** The same pass splits the other destroy, replacement and caveat sentences out of the descriptions into `Declaration.Notes` (a list of level and text) so a page reads as the AWS pages do: a one-paragraph lead and then callouts. This is mechanical but touches 37 descriptions, so it is its own step in the roadmap and can be postponed without blocking the layout.
- **Data sources** have no Import section.

### D5. Tests

**Docs.** Golden-file unit tests in `tools/docgen` over a small fixture schema: one required and one optional argument, a sensitive one, a write-only one, a nested attribute at two depths, a read-only nested field, a child resource, a singleton, a data source with lookup keys, and a resource with an import note. They pin the exact text of every section of D1 and D4, and they run in the existing `unit` job. The `docs` job (`scripts/check-docs.sh`) remains the proof that the checked-in `docs/` equals the generator's output.

**Import blocks.**

- *State of play.* `terraform-plugin-testing` v1.16.0 is the pinned and the latest release. Its `ImportStateKind` has `ImportCommandWithID`, `ImportBlockWithID` and `ImportBlockWithResourceIdentity` (`helper/resource/testing.go:459` to `469`). The block kinds run `terraform plan` on a generated configuration and check that the plan shows the import and no change. 36 of 37 resources already run the `ImportBlockWithID` step, each as one extra plan with no apply, so it adds little to a test run.
- *Required of every resource.* Add the missing `ImportBlockWithID` step to `subscription_schedule` (using the same `ImportStateVerifyIgnore` reasoning for its `start_date` and `phases.0.duration`, since a block step checks the plan, not state equality, the fields Stripe does not return need an `ignore_changes` or a configuration that omits them; see the open questions), and extend `internal/resources/layout_test.go` so that it fails when a `resource_test.go` has an import test without `ImportBlockWithID`. That is a text check, free at run time, and keeps new resources honest. The engine's shared path means one more resource-specific test would prove the same code, so no additional engine-level helper is added.
- *Identity form on a representative set.* `ImportBlockWithResourceIdentity` needs Terraform 1.12 or later or OpenTofu 1.12 or later. Add it for two resources that cover the two shapes present today: `stripe_coupon` (a top-level object with a settable ID) and `stripe_product_feature` (a child, so the parent attribute is part of the identity). The first singleton resource adds a third. The step is skipped with a version check when the CLI is older, and the CI matrix (Terraform 1.16.6, OpenTofu 1.12.3) runs it on both. Proving it on all 37 resources would add 37 plans for code that is one function in the engine (`setIdentity`).
- *Page size.* D3a: every real page under 250,000 bytes (half the Registry's 500KB limit).
- *Docs examples.* The layout test also checks that every `import.sh` uses the resource's own type name and that its ID has the shape the declaration implies (a slash for a child). The examples shown in the page are the ones the tests prove.

### D6. Alternatives inside the renderer

The renderer does not parse `Changing it replaces the resource.` out of the descriptions and does not rewrite them. If a replacement marker is wanted in the line (open question 2), it is added by having the engine's schema builder record "requires replace" in the attribute's `MarkdownDescription` as today and also in a structured form the renderer can read: the protocol schema carries no plan modifiers, so the renderer would take the flag from the declaration's field list instead (`schemagen`'s table, the same source the engine uses). That is a larger change and is deferred.

## Implementation Roadmap

### Step 1: The renderer

Create `tools/docgen`: the schema reader and conversion, the line and subsection writers, the resource, data source and provider page assemblers, and golden-file tests. It writes to a directory given by a flag and is not yet wired in. The Import section is the D4 form with no notes.

Acceptance criteria: golden tests and the page size tests of D3a pass; running it on the provider writes a tree that has a page per current resource and data source; a hand diff of three pages (`coupon`, `product_feature`, `webhook_endpoint`) against `docs/` shows only the layout changes of D1 and D2.

Status: PENDING

Dependencies: none.

### Step 2: Switch generation

Wire `internal/provider/generate.go` to `tools/docgen`, move the provider page prose to `docs-src/provider.md`, delete `templates/index.md.tmpl`, drop `tfplugindocs` from `go.mod` (`go mod tidy`), regenerate and commit `docs/`, remove the Terraform or OpenTofu binary requirement from `scripts/check-docs.sh` and its comment, and update `CONTRIBUTING.md`'s Docs section and the `docs` job of the CI notes.

Acceptance criteria: `go generate ./...` and `scripts/check-docs.sh` pass without a Terraform binary on the path; every page has the D1 order and valid front matter (D3a); a stale page is deleted; `CLAUDE.md` and `CONTRIBUTING.md` no longer name `tfplugindocs`.

Status: PENDING

Dependencies: Step 1.

### Step 3: Import notes

Add `Declaration.ImportNote` and `ImportNoteLevel`, move the import sentences out of the seven descriptions, regenerate, and extend the layout test with the `import.sh` shape check.

Acceptance criteria: the seven pages show the note in the Import section and no import sentence in the lead; `layout_test.go` fails on a wrong `import.sh` type name.

Status: PENDING

Dependencies: Step 2.

### Step 4: Import tests

Add the `ImportBlockWithID` step to `subscription_schedule`, the layout check that requires it, and the two `ImportBlockWithResourceIdentity` steps with their version skip.

Acceptance criteria: the acceptance suite passes on Terraform and OpenTofu; removing the step from any resource fails `internal/resources`.

Status: PENDING

Dependencies: none (independent of Steps 1 to 3; can be done first).

### Step 5: Lead paragraphs and callouts

Add `Declaration.Notes`, split destroy, replacement and caveat sentences out of the 37 descriptions, regenerate.

Acceptance criteria: every lead is one paragraph of at most three sentences; no sentence is lost (a reviewer compares the old and new description per resource).

Status: OPTIONAL FOLLOW-UP, not part of this ADR's done

Dependencies: Step 2.

## Architecture Documentation Updates

The repo has no `architecture/` directory yet (see the architecture ADR). When it is created, the docs pipeline (D3) and the page layout (D1, D2, D4) are described in `architecture/provider/docs.md`. `CONTRIBUTING.md` changes in Step 2 as listed; the README and `docs/` stay free of contributor material.

## Alternatives Considered

### 1. Custom `tfplugindocs` templates only

Per-resource and fallback templates (`templates/resources.md.tmpl`, `templates/data-sources.md.tmpl`) that place `SchemaMarkdown` and the import files.

**Pros:**
- No new tool; upstream keeps maintaining the rendering.
- The Import order (block, then command) and a `import-by-string-id.tf` per resource work today with the default template.

**Cons:**
- The schema is one string; Argument Reference and Attribute Reference, the `(Required)` marker and the dotted-path subsections are out of reach (see D3).
- 37 new example files would carry the import blocks, one more thing to keep in step with `import.sh`.

**Reconsider if:** `tfplugindocs` exposes the structured schema to templates, or the owner decides the Required, Optional and Read-Only split is enough.

### 2. Post-process `tfplugindocs` output

Run `tfplugindocs`, then rewrite its Markdown into the AWS layout.

**Pros:**
- Keeps upstream's schema walk.

**Cons:**
- A parser of another tool's output that breaks on its next heading change, and it still needs the Terraform CLI.

**Reconsider if:** never; it has the costs of both options.

### 3. Hand-written pages as AWS does

**Pros:** Full control of every sentence.

**Cons:** 69 resources whose argument lists drift from the schema; the architecture ADR requires generated docs checked in CI.

**Reconsider if:** the provider shrinks to a handful of resources.

### 4. Lead the Import section with `identity`, as AWS does

**Pros:** Matches AWS exactly; identity is the better form for compound IDs.

**Cons:** Needs Terraform 1.12 or OpenTofu 1.12; the `id` form works everywhere that has import blocks.

**Reconsider if:** the supported CLI floor is raised to 1.12.

### 5. Reword descriptions to the AWS style

**Pros:** Uniform voice.

**Cons:** A hand-maintained rewrite layer over Stripe's spec text that is lost or contradicted at each spec bump (see D1).

**Reconsider if:** the generator gains a reliable description pipeline of its own.

## Resolved questions

Decided by the owner agent, 2026-10-22:

1. **Replacement marker.** Keep the sentence "Changing it replaces the resource." The marker `(Optional, Forces replacement)` is a later option; it needs the engine to expose replacement in the schema in a form the renderer can read (the protocol schema carries no plan modifiers, see D6).
2. **Identity imports on OpenTofu.** Step 4's test settles it. If OpenTofu fails the identity step, the page drops the OpenTofu clause and says "Terraform v1.12.0 and later".
3. **`subscription_schedule` import-block step.** A block-step configuration that omits `start_date` and the phase `duration`.
4. **Scope.** Step 3 (import notes as callouts) is part of this ADR. Step 5 (splitting every lead into a summary plus callouts) is an optional follow-up and is not part of this ADR's done.
5. **`tfplugindocs`.** Removed entirely (Step 2).
6. **Example instance names.** Ours are kept (D1).

Also decided, as departures from the owner's brief that were accepted:

- The provider page's prose (Authentication, Stripe API version) moves from `templates/index.md.tmpl` to the plain Markdown file `docs-src/provider.md`, read by the renderer (D3).
- `internal/resources/layout_test.go` is the meta-test for per-resource requirements, including `ImportBlockWithID`; there is no new shared test helper (D5).
- Singleton import is shown in the renderer's golden tests only, because no resource page has a singleton import today; the first singleton resource adds an identity step (D4, D5).
