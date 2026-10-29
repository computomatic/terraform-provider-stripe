package stripeapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"mime/multipart"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// AttemptTimeout bounds one HTTP attempt. The operation's own deadline, set by the
// resource's timeouts block, bounds the whole call including retries.
const AttemptTimeout = 80 * time.Second

const maxResponseBytes = 64 << 20

// Host selects which Stripe host a request goes to.
type Host int

const (
	// APIHost is the main API host (`base_url`).
	APIHost Host = iota
	// FilesHost is the Files API host (`files_base_url`).
	FilesHost
)

// IsV2Path reports whether a request path belongs to the `/v2` namespace, whose bodies are
// JSON where `/v1` takes forms.
func IsV2Path(path string) bool { return strings.HasPrefix(path, "/v2/") }

// Request is one logical Stripe operation.
type Request struct {
	Method string
	// Path starts with a slash, for example `/v1/products/prod_123`. A `/v2/` path sends a
	// JSON body; any other sends a form.
	Path string
	// Params go in the query string for GET and in the body for every other method.
	Params wire.Object
	Host   Host
	// File, when set, makes a POST a multipart form upload, as the Files API takes one: Params
	// are its text parts and File its file part.
	File *File
	// Sensitive lists field names, at any depth, whose values are masked in trace logs.
	Sensitive []string
}

// File is the file part of a multipart upload.
type File struct {
	// Part is the form field that carries the file, such as `file`.
	Part string
	// Name is the file name sent with the part.
	Name    string
	Content []byte
}

// Response is a successful (2xx) Stripe response.
type Response struct {
	Status    int
	Header    http.Header
	RequestID string
	// Body is the decoded JSON body, or nil when the response had none.
	Body wire.Value
}

// Options tunes a Client beyond the provider configuration.
type Options struct {
	// Version is the provider release, sent in the User-Agent.
	Version string
	// HTTPClient replaces the default transport; its Timeout is not used.
	HTTPClient *http.Client
	// MaxInFlight caps concurrent requests; zero means DefaultMaxInFlight.
	MaxInFlight int
}

// Client is the provider's Stripe client. One Client serves a whole provider instance, so
// its limiter caps the instance's total request rate. It is safe for concurrent use.
type Client struct {
	cfg       Config
	http      *http.Client
	userAgent string
	clientUA  string
	lim       *limiter
	now       func() time.Time
	sleep     func(context.Context, time.Duration) error
	jitter    func(max time.Duration) time.Duration
	newKey    func() string
}

// New builds a Client from the resolved provider configuration.
func New(cfg Config, opts Options) *Client {
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	maxInFlight := opts.MaxInFlight
	if maxInFlight <= 0 {
		maxInFlight = DefaultMaxInFlight
	}
	version := opts.Version
	if version == "" {
		version = "dev"
	}
	c := &Client{
		cfg:       cfg,
		http:      httpClient,
		userAgent: "terraform-provider-stripe/" + version,
		clientUA:  fmt.Sprintf(`{"lang":"go","publisher":"terraform-provider-stripe","application":{"name":"terraform-provider-stripe","version":%q}}`, version),
		now:       time.Now,
		sleep:     sleepContext,
		jitter:    func(max time.Duration) time.Duration { return rand.N(max + 1) },
		newKey:    newIdempotencyKey,
	}
	c.lim = newLimiter(maxInFlight, minRequestSpacing, c.now, c.sleep)
	return c
}

// Do sends req, retrying on transport errors, 429 and 5xx up to the configured
// max_retries (see shouldRetryStatus). A non-2xx outcome is returned as *APIError. Every
// POST carries one Idempotency-Key, generated here and reused across the retries.
func (c *Client) Do(ctx context.Context, req Request) (*Response, error) {
	target, body, contentType, err := c.build(req)
	if err != nil {
		return nil, err
	}
	idempotencyKey := ""
	if req.Method == http.MethodPost {
		idempotencyKey = c.newKey()
	}

	for attempt := 0; ; attempt++ {
		resp, status, header, sendErr := c.attempt(ctx, req, target, body, contentType, idempotencyKey)

		var lastErr error
		var retry bool
		switch {
		case sendErr != nil:
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr, retry = sendErr, true
		case status >= 200 && status < 300:
			return resp, nil
		default:
			lastErr, retry = decodeAPIError(status, header, bodyOf(resp), IsV2Path(req.Path)), shouldRetryStatus(status, header)
		}

		if !retry || int64(attempt) >= c.cfg.MaxRetries {
			return nil, lastErr
		}
		wait := backoff(attempt, c.jitter)
		if after, ok := retryAfter(header, c.now()); ok && sendErr == nil {
			wait = after
		}
		if deadline, ok := ctx.Deadline(); ok && c.now().Add(wait).After(deadline) {
			return nil, lastErr
		}
		tflog.Debug(withFields(ctx, "method", req.Method, "path", req.Path,
			"attempt", strconv.Itoa(attempt+1), "wait", wait.String()), "stripe request will be retried")
		if err := c.sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
}

func bodyOf(r *Response) wire.Value {
	if r == nil {
		return nil
	}
	return r.Body
}

// build resolves the URL, body and content type of a request. They are computed once and
// reused by every attempt.
func (c *Client) build(req Request) (target string, body []byte, contentType string, err error) {
	base := c.cfg.BaseURL
	if req.Host == FilesHost {
		base = c.cfg.FilesBaseURL
	}
	target = base + req.Path

	if req.Method == http.MethodGet {
		if len(req.Params) > 0 {
			query, err := wire.EncodeForm(req.Params)
			if err != nil {
				return "", nil, "", err
			}
			target += "?" + query
		}
		return target, nil, "", nil
	}

	if req.File != nil {
		body, contentType, err := encodeMultipart(req.Params, req.File)
		if err != nil {
			return "", nil, "", err
		}
		return target, body, contentType, nil
	}
	if IsV2Path(req.Path) {
		// The conformance suite sends no body and no content type when there is nothing to
		// send, and the fake refuses a body on a DELETE.
		if len(req.Params) == 0 {
			return target, nil, "", nil
		}
		encoded, err := wire.EncodeJSON(req.Params)
		if err != nil {
			return "", nil, "", err
		}
		return target, encoded, "application/json", nil
	}
	encoded, err := wire.EncodeForm(req.Params)
	if err != nil {
		return "", nil, "", err
	}
	return target, []byte(encoded), "application/x-www-form-urlencoded", nil
}

// encodeMultipart renders params as the text parts of a multipart form, named in the form
// body's bracket notation, followed by the file part. Every attempt resends the same bytes, so a
// retry repeats the upload exactly.
func encodeMultipart(params wire.Object, file *File) (body []byte, contentType string, err error) {
	encoded, err := wire.EncodeForm(params)
	if err != nil {
		return nil, "", err
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for pair := range strings.SplitSeq(encoded, "&") {
		if pair == "" {
			continue
		}
		rawKey, rawValue, _ := strings.Cut(pair, "=")
		key, errKey := url.QueryUnescape(rawKey)
		value, errValue := url.QueryUnescape(rawValue)
		if err := errors.Join(errKey, errValue); err != nil {
			return nil, "", err
		}
		if err := w.WriteField(key, value); err != nil {
			return nil, "", err
		}
	}
	part, err := w.CreateFormFile(file.Part, file.Name)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(file.Content); err != nil {
		return nil, "", err
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

// attempt performs one HTTP exchange. On a received response it returns the decoded body
// in resp (whatever the status); sendErr is set only when no response was received or
// readable.
func (c *Client) attempt(ctx context.Context, req Request, target string, body []byte, contentType, idempotencyKey string) (resp *Response, status int, header http.Header, sendErr error) {
	release, err := c.lim.acquire(ctx)
	if err != nil {
		return nil, 0, nil, err
	}
	defer release()

	attemptCtx, cancel := context.WithTimeout(ctx, AttemptTimeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(attemptCtx, req.Method, target, bytes.NewReader(body))
	if err != nil {
		return nil, 0, nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	httpReq.Header.Set("Stripe-Version", Version)
	httpReq.Header.Set("User-Agent", c.userAgent)
	httpReq.Header.Set("X-Stripe-Client-User-Agent", c.clientUA)
	if contentType != "" {
		httpReq.Header.Set("Content-Type", contentType)
	}
	if c.cfg.StripeAccount != "" {
		httpReq.Header.Set("Stripe-Account", c.cfg.StripeAccount)
	}
	if c.cfg.StripeContext != "" {
		httpReq.Header.Set("Stripe-Context", c.cfg.StripeContext)
	}
	if idempotencyKey != "" {
		httpReq.Header.Set("Idempotency-Key", idempotencyKey)
	}

	logCtx := withFields(ctx, "method", req.Method, "path", req.Path)
	tflog.Debug(logCtx, "stripe request")
	if len(req.Params) > 0 {
		tflog.Trace(withFields(logCtx, "body", renderBody(req.Params, req.Sensitive)), "stripe request body")
	}

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("stripe request %s %s failed: %w", req.Method, req.Path, redactKey(err, c.cfg.APIKey))
	}
	defer httpResp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(httpResp.Body, maxResponseBytes))
	if err != nil {
		return nil, 0, nil, fmt.Errorf("stripe response %s %s unreadable: %w", req.Method, req.Path, err)
	}
	requestID := httpResp.Header.Get("Request-Id")
	tflog.Debug(withFields(logCtx, "status", strconv.Itoa(httpResp.StatusCode), "request_id", requestID), "stripe response")

	resp = &Response{Status: httpResp.StatusCode, Header: httpResp.Header, RequestID: requestID}
	if len(bytes.TrimSpace(raw)) > 0 {
		// A body that is not JSON (an HTML 502 page) leaves Body nil; the status still decides.
		if decoded, err := wire.DecodeJSON(raw); err == nil {
			resp.Body = decoded
			tflog.Trace(withFields(logCtx, "body", renderBody(decoded, req.Sensitive)), "stripe response body")
		} else if httpResp.StatusCode >= 200 && httpResp.StatusCode < 300 {
			return nil, 0, nil, fmt.Errorf("stripe response %s %s is not valid JSON: %w", req.Method, req.Path, err)
		}
	}
	return resp, httpResp.StatusCode, httpResp.Header, nil
}

// withFields adds name/value pairs to the structured log fields of ctx.
func withFields(ctx context.Context, pairs ...string) context.Context {
	for i := 0; i+1 < len(pairs); i += 2 {
		ctx = tflog.SetField(ctx, pairs[i], pairs[i+1])
	}
	return ctx
}

// renderBody encodes a body for trace logs with the sensitive fields masked.
func renderBody(v wire.Value, sensitive []string) string {
	encoded, err := wire.EncodeJSON(mask(v, sensitive))
	if err != nil {
		return "<unrenderable>"
	}
	return string(encoded)
}

// mask returns v with the value of every field named in sensitive replaced.
func mask(v wire.Value, sensitive []string) wire.Value {
	switch t := v.(type) {
	case wire.Array:
		out := make(wire.Array, len(t))
		for i, item := range t {
			out[i] = mask(item, sensitive)
		}
		return out
	case wire.Object:
		out := make(wire.Object, len(t))
		for i, f := range t {
			if slices.Contains(sensitive, f.Name) {
				out[i] = wire.Field{Name: f.Name, Value: wire.String("***")}
			} else {
				out[i] = wire.Field{Name: f.Name, Value: mask(f.Value, sensitive)}
			}
		}
		return out
	default:
		return v
	}
}

// redactKey keeps the API key out of an error message, in case a transport echoes it.
func redactKey(err error, key string) error {
	if key == "" || !strings.Contains(err.Error(), key) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), key, "***"))
}
