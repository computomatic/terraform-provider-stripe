package stripeapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"

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
	// InvalidFields is the v2 `invalid_fields` array as Stripe sent it, or nil.
	InvalidFields wire.Value
	// RateLimitedReason is the Stripe-Rate-Limited-Reason header of a 429, if any.
	RateLimitedReason string
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
func decodeAPIError(status int, header http.Header, body wire.Value) *APIError {
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
			if fields, ok := inner.Get("invalid_fields"); ok {
				e.InvalidFields = fields
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

// AddToDiagnostics maps err into diags. A Stripe error becomes the summary
// `Stripe API error: <code or type>`, with the message, the request ID and any v2 invalid
// fields as detail. When the error names a param and hasAttribute reports that the
// matching attribute exists, the diagnostic is attached to it. hasAttribute may be nil.
func AddToDiagnostics(diags *diag.Diagnostics, err error, hasAttribute func(path.Path) bool) {
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

	var detail strings.Builder
	detail.WriteString(apiErr.Message)
	if apiErr.RateLimitedReason != "" {
		detail.WriteString("\n\nRate limited: " + apiErr.RateLimitedReason)
	}
	if apiErr.InvalidFields != nil {
		if encoded, encErr := wire.EncodeJSON(apiErr.InvalidFields); encErr == nil {
			detail.WriteString("\n\nInvalid fields: " + string(encoded))
		}
	}
	if apiErr.RequestID != "" {
		detail.WriteString("\n\nRequest-Id: " + apiErr.RequestID)
	}

	if apiErr.Param != "" && hasAttribute != nil {
		if p, ok := ParamPath(apiErr.Param); ok && hasAttribute(p) {
			diags.AddAttributeError(p, summary, detail.String())
			return
		}
	}
	diags.AddError(summary, detail.String())
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
