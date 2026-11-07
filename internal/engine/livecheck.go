package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// ProviderData is what the provider hands every resource and data source.
type ProviderData struct {
	Client Client
	// Live decides whether a resource that moves money may change an object.
	Live *LiveCheck
	// Country is the country of the account the provider configuration calls, which a recorded
	// refusal holds only in (recorded.go).
	Country *AccountCountry
	// Strict turns the warnings of an apply that Stripe answered otherwise than planned (E4)
	// and of a field in an unexpected shape (E6) into errors. The acceptance harness sets it,
	// so that a test fails where Stripe or the fake answers otherwise than the conformance
	// suite recorded.
	Strict bool
}

// LiveCheck answers, once per provider configuration, whether money may move: always when the
// provider allows live-mode money movement, otherwise only once Stripe reports that the
// account is not in live mode (adrs/1-pending/2026-10-22-money-moving-resources, D2). The
// key is never read for the mode.
type LiveCheck struct {
	allow  bool
	client Client
	// account is the connected account the client sends as `Stripe-Account`, or "".
	account string
	mode    onceRead[bool]
}

// NewLiveCheck returns the check of one provider configuration.
func NewLiveCheck(client Client, allow bool, account string) *LiveCheck {
	return &LiveCheck{allow: allow, client: client, account: account, mode: onceRead[bool]{read: func(ctx context.Context) (bool, error) { return readLiveMode(ctx, client) }}}
}

const (
	liveRefusedSummary = "Live-mode money movement is not allowed"
	cannotTellSummary  = "Cannot tell whether the Stripe account is in live mode"
)

// refusal is the error that stops a change to a resource of typeName, or nil when the change
// may go ahead. The first call reads the balance; later and concurrent calls wait for it and
// reuse its answer, a failure from Stripe included. A read that ends with its caller's context
// is not kept, so the next call reads again.
func (c *LiveCheck) refusal(ctx context.Context, typeName string) diag.Diagnostic {
	if c.allow {
		return nil
	}
	live, err := c.mode.get(ctx)
	thing := WithArticle(Noun(typeName))
	if err != nil {
		return diag.NewErrorDiagnostic(cannotTellSummary, fmt.Sprintf(
			"The provider creates or changes %s only once Stripe has confirmed the account's mode, because it can move real money. Reading GET /v1/balance failed:\n\n%s\n\nA restricted key needs the Balance read permission for this check.",
			thing, describeError(err)))
	}
	if !live {
		return nil
	}
	account := "the account this provider configuration uses"
	if c.account != "" {
		account = "the connected account `" + c.account + "` this provider configuration uses"
	}
	return diag.NewErrorDiagnostic(liveRefusedSummary, fmt.Sprintf(
		"Stripe reports that %s is in live mode (GET /v1/balance answered livemode: true). Creating or changing %s can move real money, so the provider does not do it in live mode unless the provider block sets allow_live_money_movement = true.\n\nTo manage test data, configure this provider with a sandbox or test-mode key.",
		account, thing))
}

// onceRead is a value one provider configuration reads from Stripe once. The first get reads
// it; later and concurrent calls wait for that read and reuse its answer, a failure from Stripe
// included. A read that ends with its caller's context is not kept, so the next call reads
// again, and each waiting call waits only until its own context ends.
type onceRead[T any] struct {
	read func(context.Context) (T, error)

	mu      sync.Mutex
	checked bool
	value   T
	err     error
	// reading is closed when the read in flight ends, and nil when none is.
	reading chan struct{}
}

func (c *onceRead[T]) get(ctx context.Context) (T, error) {
	for {
		c.mu.Lock()
		if c.checked {
			value, err := c.value, c.err
			c.mu.Unlock()
			return value, err
		}
		if wait := c.reading; wait != nil {
			c.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				var zero T
				return zero, ctx.Err()
			}
		}
		done := make(chan struct{})
		c.reading = done
		c.mu.Unlock()

		value, err := c.read(ctx)
		c.mu.Lock()
		if ctx.Err() == nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			c.value, c.err, c.checked = value, err, true
		}
		c.reading = nil
		close(done)
		c.mu.Unlock()
		return value, err
	}
}

// readLiveMode reads the account's mode from its balance. Anything but a boolean `livemode`
// is an error: the answer is never assumed.
func readLiveMode(ctx context.Context, client Client) (bool, error) {
	resp, err := client.Do(ctx, stripeapi.Request{Method: "GET", Path: "/v1/balance"})
	if err != nil {
		return false, err
	}
	body, ok := resp.Body.(wire.Object)
	if !ok {
		return false, errors.New("Stripe did not answer with a balance object")
	}
	live, ok := body.GetBool("livemode")
	if !ok {
		return false, errors.New("the balance Stripe returned has no boolean `livemode`")
	}
	return live, nil
}

// describeError is err as a user reads it, with Stripe's request ID when there is one.
func describeError(err error) string {
	var apiErr *stripeapi.APIError
	if errors.As(err, &apiErr) && apiErr.RequestID != "" {
		return err.Error() + "\nRequest-Id: " + apiErr.RequestID
	}
	return err.Error()
}

// moneyNote is the page's callout for a resource that moves money, without its sigil, or ""
// for one that does not.
func (m *model) moneyNote() string {
	if !m.decl.MovesMoney {
		return ""
	}
	thing := Noun(m.decl.TypeName)
	lead := WithArticle(thing)
	status := ""
	if len(m.decl.Transitions) > 0 {
		n, _ := m.top(m.decl.Transitions[0].Field)
		status = ", including setting its `" + n.AttrName() + "`,"
	}
	return strings.ToUpper(lead[:1]) + lead[1:] + " can move real money. In a live-mode Stripe account, creating or changing one" + status +
		" fails at plan time unless the provider sets `allow_live_money_movement = true`. Reading or importing one never does. The provider asks Stripe for the account's mode. See the Managing Test Data guide."
}
