# Architecture documentation updates

The repository has no `architecture/` directory yet; the provider architecture ADR initialises it. This ADR adds one section to `architecture/provider/mapping.md`, after "Rules" and before "Provider configuration". `architecture/README.md` is unchanged.

## `architecture/provider/mapping.md` (new section)

~~~~~markdown
## Stripe `/v2` resources

A declaration is a `/v2` resource when its paths start with `/v2/`. The client then sends JSON bodies; the engine builds the same request tree as for `/v1`.

|Case|Create|Update|
|-|-|-|
|Null or unknown value|Omitted|Omitted unless the field changed|
|Empty list, set or object|Omitted unless the field is required|Sent whole when it changed|
|Changed list or nested object|Sent whole|Sent whole, never a partial patch|
|`metadata`|Sent as configured|Changed keys only; a removed key is `null`|
|Removed scalar|Not applicable|Refused unless the declaration lists the field in `ClearsWithNull`|

Stripe hides some `/v2` fields unless the request includes them (`webhook_endpoint.url`). A declaration lists them in `Include`, and the engine sends them on every call that returns the object, so the value is read back and drift in it is detected. `include` is never an attribute.

Errors carry `code`, `message` and, for field validation, `invalid_fields`; each entry attaches to the attribute it names when that attribute exists. Context fields Stripe adds to an error are kept in the diagnostic detail. A `/v2` list returns `data`, `next_page_url` and `previous_page_url` and has no `has_more`; a lookup reads one page and treats a non-null `next_page_url` as more than one match.
~~~~~
