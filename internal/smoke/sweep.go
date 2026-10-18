// Package smoke holds the sandbox smoke run's sweep, which removes what a smoke run
// leaves behind. See the smoke run ADR, "Avoiding junk".
package smoke

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// Prefix starts the name of everything a smoke run creates. The sweep touches nothing else.
const Prefix = "tfsmoke-"

// lookback is how far back the sweep looks where a list accepts a created filter. The run
// is weekly, so a run killed before its closing sweep is swept by the next one a week
// later; two weeks also covers a skipped or displaced week.
const lookback = 14 * 24 * time.Hour

const pageSize = 100

// action is what the sweep does to an object.
type action string

const (
	actionDelete     action = "delete"
	actionArchive    action = "archive"
	actionDeactivate action = "deactivate"
)

// objectType is one kind of object the sweep finds by name.
type objectType struct {
	name string
	path string
	// nameField holds the text that carries the prefix.
	nameField string
	// createdFilter says the list endpoint accepts created[gte]. Without it the sweep
	// walks the whole list. This is the one place the per-type choice lives, so the
	// findings of the live probes (ADR Step 2) change it here.
	createdFilter bool
	// act removes the object and returns what it did, or "" when nothing was left to do.
	act func(ctx context.Context, s *Sweeper, t objectType, id string, obj wire.Object) (action, error)
	// skip reports an object that is already inactive and needs nothing.
	skip func(obj wire.Object) bool
}

func notInactive(wire.Object) bool { return false }

var objectTypes = []objectType{
	{name: "customer", path: "/v1/customers", nameField: "name", createdFilter: true, skip: notInactive, act: deleter},
	{name: "coupon", path: "/v1/coupons", nameField: "name", createdFilter: true, skip: notInactive, act: deleter},
	{name: "webhook_endpoint", path: "/v1/webhook_endpoints", nameField: "description", createdFilter: false, skip: notInactive, act: deleter},
	{name: "product", path: "/v1/products", nameField: "name", createdFilter: true, skip: notInactive, act: sweepProduct},
	{name: "billing_meter", path: "/v1/billing/meters", nameField: "display_name", createdFilter: false, skip: meterInactive, act: deactivateMeter},
}

func archived(obj wire.Object) bool {
	active, ok := obj.GetBool("active")
	return ok && !active
}

func meterInactive(obj wire.Object) bool {
	status, _ := obj.GetString("status")
	return status == "inactive"
}

// Swept is one object the sweep acted on.
type Swept struct {
	Type   string
	ID     string
	Name   string
	Action string
}

func (s Swept) String() string {
	return fmt.Sprintf("%s %s name=%q action=%s", s.Type, s.ID, s.Name, s.Action)
}

// Sweeper deletes or archives the objects a smoke run made.
type Sweeper struct {
	Client *stripeapi.Client
	// Match decides whether a name belongs to a smoke run. It defaults to HasPrefix.
	Match func(name string) bool
	// Now is the clock for the created filter. It defaults to time.Now.
	Now func() time.Time
	// Log receives one line per object acted on. It must never be given a key.
	Log func(format string, args ...any)

	swept []Swept
}

// HasPrefix is the match: the name starts with the smoke prefix, case sensitively. A name
// that merely contains it does not belong to a smoke run.
func HasPrefix(name string) bool { return strings.HasPrefix(name, Prefix) }

// Run sweeps every type and returns what it acted on. A failure on one object or type
// does not stop the rest; the error lists every failure with the object id and Stripe's
// message.
func (s *Sweeper) Run(ctx context.Context) ([]Swept, error) {
	if s.Match == nil {
		s.Match = HasPrefix
	}
	if s.Now == nil {
		s.Now = time.Now
	}
	s.swept = nil
	var errs []error
	for _, t := range objectTypes {
		errs = append(errs, s.sweepType(ctx, t)...)
	}
	return s.swept, errors.Join(errs...)
}

func (s *Sweeper) sweepType(ctx context.Context, t objectType) []error {
	objects, err := s.list(ctx, t.path, t.createdFilter, nil)
	if err != nil {
		return []error{fmt.Errorf("listing %s: %w", t.name, err)}
	}
	var errs []error
	for _, obj := range objects {
		name, _ := obj.GetString(t.nameField)
		id, _ := obj.GetString("id")
		if id == "" || !s.Match(name) || t.skip(obj) {
			continue
		}
		did, err := t.act(ctx, s, t, id, obj)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s %s: %w", t.name, id, err))
			continue
		}
		if did != "" {
			s.record(Swept{Type: t.name, ID: id, Name: name, Action: string(did)})
		}
	}
	return errs
}

func (s *Sweeper) record(o Swept) {
	s.swept = append(s.swept, o)
	if s.Log != nil {
		s.Log("swept %s", o)
	}
}

// list walks every page of path. All pages are read before anything is changed, so
// removing an object cannot disturb the cursor.
func (s *Sweeper) list(ctx context.Context, path string, withCreated bool, extra wire.Object) ([]wire.Object, error) {
	params := append(wire.Object{{Name: "limit", Value: wire.Int(pageSize)}}, extra...)
	if withCreated {
		since := s.Now().Add(-lookback).Unix()
		params = append(params, wire.Field{Name: "created", Value: wire.Object{{Name: "gte", Value: wire.Int(since)}}})
	}
	var out []wire.Object
	for {
		resp, err := s.Client.Do(ctx, stripeapi.Request{Method: "GET", Path: path, Params: params})
		if err != nil {
			return nil, err
		}
		page, ok := resp.Body.(wire.Object)
		if !ok {
			return nil, fmt.Errorf("GET %s: the response is not an object", path)
		}
		data, _ := page.Get("data")
		items, _ := data.(wire.Array)
		for _, item := range items {
			if obj, ok := item.(wire.Object); ok {
				out = append(out, obj)
			}
		}
		more, _ := page.GetBool("has_more")
		if !more || len(items) == 0 {
			return out, nil
		}
		last, _ := items[len(items)-1].(wire.Object).GetString("id")
		params = withCursor(params, last)
	}
}

func withCursor(params wire.Object, id string) wire.Object {
	out := make(wire.Object, 0, len(params)+1)
	for _, f := range params {
		if f.Name != "starting_after" {
			out = append(out, f)
		}
	}
	return append(out, wire.Field{Name: "starting_after", Value: wire.String(id)})
}

// send makes a change and treats an object that is already gone as done.
func (s *Sweeper) send(ctx context.Context, method, path string, params wire.Object) error {
	_, err := s.Client.Do(ctx, stripeapi.Request{Method: method, Path: path, Params: params})
	if stripeapi.IsNotFound(err) {
		return nil
	}
	return err
}

func deleter(ctx context.Context, s *Sweeper, t objectType, id string, obj wire.Object) (action, error) {
	return actionDelete, s.send(ctx, "DELETE", t.path+"/"+id, nil)
}

func deactivateMeter(ctx context.Context, s *Sweeper, t objectType, id string, obj wire.Object) (action, error) {
	return actionDeactivate, s.send(ctx, "POST", t.path+"/"+id+"/deactivate", nil)
}

// sweepProduct archives the product's active prices, whether or not the product is
// already archived (destroy archives a product and leaves a price made out of band
// active), then archives the product if it is still active. A product with no price at all
// is deleted, which Stripe only allows then.
func sweepProduct(ctx context.Context, s *Sweeper, t objectType, id string, obj wire.Object) (action, error) {
	prices, err := s.list(ctx, "/v1/prices", false, wire.Object{{Name: "product", Value: wire.String(id)}})
	if err != nil {
		return "", fmt.Errorf("listing prices: %w", err)
	}
	var errs []error
	for _, price := range prices {
		priceID, _ := price.GetString("id")
		if archived(price) {
			continue
		}
		if owner, _ := price.GetString("product"); owner != id {
			errs = append(errs, fmt.Errorf("price %s belongs to product %q, not %s: left alone", priceID, owner, id))
			continue
		}
		if err := s.send(ctx, "POST", "/v1/prices/"+priceID, wire.Object{{Name: "active", Value: wire.Bool(false)}}); err != nil {
			errs = append(errs, fmt.Errorf("price %s: %w", priceID, err))
			continue
		}
		name, _ := price.GetString("nickname")
		s.record(Swept{Type: "price", ID: priceID, Name: name, Action: string(actionArchive)})
	}
	if err := errors.Join(errs...); err != nil {
		return "", err
	}
	if len(prices) == 0 {
		return actionDelete, s.send(ctx, "DELETE", t.path+"/"+id, nil)
	}
	if archived(obj) {
		return "", nil
	}
	return actionArchive, s.send(ctx, "POST", t.path+"/"+id, wire.Object{{Name: "active", Value: wire.Bool(false)}})
}
