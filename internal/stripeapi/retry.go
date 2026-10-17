package stripeapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	backoffBase = 250 * time.Millisecond
	backoffCap  = 8 * time.Second
	// maxRetryAfter bounds a server-supplied Retry-After so a bad header cannot stall an
	// operation that has no deadline.
	maxRetryAfter = 60 * time.Second
)

// shouldRetryStatus decides whether a response is worth another attempt. A
// Stripe-Should-Retry header overrides the status rule; without one, 429 and 5xx retry
// and every other status does not. A 429 retries whether it is a rate limit or a
// lock_timeout, since Stripe says both retry with backoff.
func shouldRetryStatus(status int, header http.Header) bool {
	switch strings.ToLower(header.Get("Stripe-Should-Retry")) {
	case "true":
		return true
	case "false":
		return false
	}
	return status == http.StatusTooManyRequests || status >= 500
}

// backoff is the wait before retry number attempt (0 for the first retry): full jitter
// over an exponentially growing window capped at backoffCap.
func backoff(attempt int, jitter func(max time.Duration) time.Duration) time.Duration {
	window := backoffCap
	if attempt < 6 {
		window = min(backoffBase<<attempt, backoffCap)
	}
	return jitter(window)
}

// retryAfter reads a Retry-After header as seconds or an HTTP date.
func retryAfter(header http.Header, now time.Time) (time.Duration, bool) {
	raw := header.Get("Retry-After")
	if raw == "" {
		return 0, false
	}
	if secs, err := strconv.ParseFloat(raw, 64); err == nil && secs >= 0 {
		return min(time.Duration(secs*float64(time.Second)), maxRetryAfter), true
	}
	if at, err := http.ParseTime(raw); err == nil {
		return min(max(at.Sub(now), 0), maxRetryAfter), true
	}
	return 0, false
}
