package stripeapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// APIError is an error response from Stripe. A field Stripe omitted is empty or nil, never
// defaulted: many errors carry neither a code nor a param.
type APIError struct {
	Status    int
	RequestID string
	Type      string
	Code      string
	Message   string
	// Param names the request parameter at fault in bracket notation (v1).
	Param string
	// InvalidFields is the v2 `invalid_fields` list, entry by entry, or nil.
	InvalidFields []InvalidField
	// Context holds the members of a v2 error beyond the ones above, such as `field` or
	// `event_type`, and any `invalid_fields` entry that does not have the usual shape.
	Context wire.Object
	// RateLimitedReason is the Stripe-Rate-Limited-Reason header of a 429, if any.
	RateLimitedReason string
}

// InvalidField is one entry of a v2 `invalid_fields` list. Field is a dotted path.
type InvalidField struct {
	Field   string
	Message string
}

// knownErrorMembers are the members of a Stripe error that APIError has a place for or
// that carry no information for a user.
var knownErrorMembers = map[string]bool{
	"type": true, "code": true, "message": true, "param": true, "invalid_fields": true,
	"doc_url": true, "request_log_url": true, "user_message": true,
}

func (e *APIError) Error() string {
	return fmt.Sprintf("stripe: %s (HTTP %d)", e.Message, e.Status)
}

// IsNotFound reports whether Stripe answered 404.
func (e *APIError) IsNotFound() bool { return e.Status == http.StatusNotFound }

// IsNotFound reports whether err is an APIError with status 404.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.IsNotFound()
}

// decodeAPIError builds an APIError from a failed response. A body that is not Stripe's
// error envelope still yields an error that carries the status and a generic message.
// Only a `/v2` error gets InvalidFields and Context. A `/v1` error carries other members
// (`payment_intent`, `charge`, `source`) that hold objects and secrets, and none of them
// may reach a diagnostic.
func decodeAPIError(status int, header http.Header, body wire.Value, v2 bool) *APIError {
	e := &APIError{
		Status:            status,
		RequestID:         header.Get("Request-Id"),
		RateLimitedReason: header.Get("Stripe-Rate-Limited-Reason"),
	}
	if obj, ok := body.(wire.Object); ok {
		if inner, ok := obj.GetObject("error"); ok {
			e.Type, _ = inner.GetString("type")
			e.Code, _ = inner.GetString("code")
			e.Message, _ = inner.GetString("message")
			e.Param, _ = inner.GetString("param")
			if v2 {
				e.InvalidFields, e.Context = decodeInvalidFields(inner)
			}
		}
	}
	if e.Message == "" {
		e.Message = http.StatusText(status)
		if e.Message == "" {
			e.Message = "unexpected response"
		}
	}
	return e
}

// scalarContext reports whether a context member is a scalar or an array of scalars. An
// object is never kept: it may hold anything Stripe attaches to an error.
func scalarContext(v wire.Value) bool {
	switch t := v.(type) {
	case wire.String, wire.Int, wire.Bool, wire.Decimal:
		return true
	case wire.Array:
		for _, item := range t {
			switch item.(type) {
			case wire.String, wire.Int, wire.Bool, wire.Decimal:
			default:
				return false
			}
		}
		return true
	}
	return false
}

// decodeInvalidFields reads the `invalid_fields` list of a v2 error and the context members
// beside it. An entry without a string `field` is kept in the context as Stripe sent it.
func decodeInvalidFields(inner wire.Object) ([]InvalidField, wire.Object) {
	var fields []InvalidField
	var context, odd wire.Object
	for _, f := range inner {
		if !knownErrorMembers[f.Name] && scalarContext(f.Value) {
			context = append(context, f)
		}
	}
	var unparsed wire.Array
	if list, ok := inner.Get("invalid_fields"); ok {
		entries, isArray := list.(wire.Array)
		if !isArray && scalarContext(list) {
			unparsed = wire.Array{list}
		}
		for _, entry := range entries {
			obj, isObj := entry.(wire.Object)
			name, hasField := "", false
			if isObj {
				name, hasField = obj.GetString("field")
			}
			if !hasField {
				if scalarContext(entry) {
					unparsed = append(unparsed, entry)
				}
				continue
			}
			message, _ := obj.GetString("message")
			fields = append(fields, InvalidField{Field: name, Message: message})
		}
	}
	if len(unparsed) > 0 {
		odd = wire.Object{{Name: "invalid_fields", Value: unparsed}}
	}
	return fields, append(context, odd...)
}

// AttributeType returns the type of the attribute at a path in the resource's schema, and
// false when the schema has none there.
type AttributeType func(path.Path) (attr.Type, bool)

// AddToDiagnostics maps err into diags. A Stripe error becomes the summary
// `Stripe API error: <code or type>`, with the message, the request ID and any v2 context
// as detail. When the error names a param (v1) and hasAttribute reports that the matching
// attribute exists, the diagnostic is attached to it. Each v2 invalid field is attached
// to the attribute it names, or to the longest prefix of it that is an attribute; the
// ones that match nothing stay in the general diagnostic. typeAt may be nil.
func AddToDiagnostics(diags *diag.Diagnostics, err error, typeAt AttributeType) {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		diags.AddError("Stripe request failed", err.Error())
		return
	}

	summary := "Stripe API error: "
	switch {
	case apiErr.Code != "":
		summary += apiErr.Code
	case apiErr.Type != "":
		summary += apiErr.Type
	default:
		summary += "HTTP " + strconv.Itoa(apiErr.Status)
	}

	var suffix strings.Builder
	if apiErr.RateLimitedReason != "" {
		suffix.WriteString("\n\nRate limited: " + apiErr.RateLimitedReason)
	}
	if len(apiErr.Context) > 0 {
		if encoded, encErr := wire.EncodeJSON(apiErr.Context); encErr == nil {
			suffix.WriteString("\n\nContext: " + string(encoded))
		}
	}
	if apiErr.RequestID != "" {
		suffix.WriteString("\n\nRequest-Id: " + apiErr.RequestID)
	}

	var unattached []InvalidField
	for _, f := range apiErr.InvalidFields {
		if p, ok := attributeFor(f.Field, typeAt); ok {
			diags.AddAttributeError(p, summary, f.Field+": "+f.Message+suffix.String())
			continue
		}
		unattached = append(unattached, f)
	}
	if len(apiErr.InvalidFields) > 0 && len(unattached) == 0 {
		return
	}

	var detail strings.Builder
	detail.WriteString(apiErr.Message)
	for _, f := range unattached {
		detail.WriteString("\n" + f.Field + ": " + f.Message)
	}
	detail.WriteString(suffix.String())

	if apiErr.Param != "" && typeAt != nil {
		if p, ok := ParamPath(apiErr.Param); ok {
			if _, exists := typeAt(p); exists {
				diags.AddAttributeError(p, summary, detail.String())
				return
			}
		}
	}
	diags.AddError(summary, detail.String())
}

// attributeFor converts a v2 field such as `webhook_endpoint.url` into the path of the
// deepest attribute it names. A segment of digits is a list index. The walk stops at a set
// or a map, because an element of a set has no stable index and a map key is data, not an
// attribute: an error about one attaches to the set or map itself.
func attributeFor(field string, typeAt AttributeType) (path.Path, bool) {
	if typeAt == nil || field == "" {
		return path.Empty(), false
	}
	var found path.Path
	var ok bool
	p := path.Empty()
	for i, seg := range strings.Split(field, ".") {
		switch {
		case seg == "":
			return path.Empty(), false
		case i == 0:
			p = path.Root(seg)
		default:
			if n, err := strconv.Atoi(seg); err == nil {
				p = p.AtListIndex(n)
			} else {
				p = p.AtName(seg)
			}
		}
		typ, exists := typeAt(p)
		if !exists {
			break
		}
		found, ok = p, true
		switch typ.(type) {
		case basetypes.SetTypable, basetypes.MapTypable:
			return found, true
		}
	}
	return found, ok
}

// ParamPath converts a Stripe param such as `items[0][price]` into an attribute path:
// numeric segments become list indexes and the rest become attribute names.
func ParamPath(param string) (path.Path, bool) {
	name, rest, _ := strings.Cut(param, "[")
	if name == "" {
		return path.Empty(), false
	}
	p := path.Root(name)
	for rest != "" {
		seg, after, closed := strings.Cut(rest, "]")
		if !closed || seg == "" {
			return path.Empty(), false
		}
		if i, err := strconv.Atoi(seg); err == nil {
			p = p.AtListIndex(i)
		} else {
			p = p.AtName(seg)
		}
		if after != "" && !strings.HasPrefix(after, "[") {
			return path.Empty(), false
		}
		rest = strings.TrimPrefix(after, "[")
	}
	return p, true
}
