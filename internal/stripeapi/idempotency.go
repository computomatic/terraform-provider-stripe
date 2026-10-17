package stripeapi

import (
	cryptorand "crypto/rand"
	"fmt"
)

// newIdempotencyKey returns a random version 4 UUID, 36 characters, well inside Stripe's
// 255-character limit.
func newIdempotencyKey() string {
	var b [16]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("stripeapi: no randomness for an idempotency key: %v", err))
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
