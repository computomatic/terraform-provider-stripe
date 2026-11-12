package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// AccountCountry is the country of the account a provider configuration calls: the platform's,
// or the connected account's that the client sends as `Stripe-Account`. It is read from
// GET /v1/account once per provider configuration, and only when a plan matches a recorded
// refusal, so that other plans ask Stripe nothing.
type AccountCountry struct{ country onceRead[string] }

// NewAccountCountry returns the country of the account client calls, unread.
func NewAccountCountry(client Client) *AccountCountry {
	return &AccountCountry{country: onceRead[string]{read: func(ctx context.Context) (string, error) { return readCountry(ctx, client) }}}
}

// readCountry reads the account's country. Anything but a non-empty string is an error.
func readCountry(ctx context.Context, client Client) (string, error) {
	resp, err := client.Do(ctx, stripeapi.Request{Method: "GET", Path: "/v1/account"})
	if err != nil {
		return "", err
	}
	body, ok := resp.Body.(wire.Object)
	if !ok {
		return "", errors.New("Stripe did not answer with an account object")
	}
	country, ok := body.GetString("country")
	if !ok || country == "" {
		return "", errors.New("the account Stripe returned has no `country`")
	}
	return country, nil
}

// in reports whether the account is in country. An account it cannot read is in none.
func (c *AccountCountry) in(ctx context.Context, country string) bool {
	if c == nil {
		return false
	}
	got, err := c.country.get(ctx)
	return err == nil && strings.EqualFold(got, country)
}

// recordedRefusal is a request the conformance suite recorded Stripe refusing, as the plan
// matches it (fields.PlanRefusal).
type recordedRefusal struct {
	fields.PlanRefusal
	sends, holds wire.Object
}

// recordedRefusals decodes the refused requests the table puts on the nodes' fields, and
// refuses one whose pattern is not a JSON object.
func recordedRefusals(nodes []*node) ([]recordedRefusal, error) {
	var out []recordedRefusal
	for _, n := range nodes {
		for _, r := range n.PlanRefusals {
			sends, err := patternObject(r.Sends)
			if err != nil {
				return nil, fmt.Errorf("`%s`: the refusal of probe %s: %w", n.Path, r.Probe, err)
			}
			holds, err := patternObject(r.Holds)
			if err != nil {
				return nil, fmt.Errorf("`%s`: the refusal of probe %s: %w", n.Path, r.Probe, err)
			}
			if len(sends) == 0 {
				return nil, fmt.Errorf("`%s`: the refusal of probe %s sends nothing", n.Path, r.Probe)
			}
			out = append(out, recordedRefusal{PlanRefusal: r, sends: sends, holds: holds})
		}
		kids, err := recordedRefusals(n.Kids)
		if err != nil {
			return nil, err
		}
		out = append(out, kids...)
	}
	return out, nil
}

func patternObject(s string) (wire.Object, error) {
	if s == "" {
		return nil, nil
	}
	v, err := wire.DecodeJSON([]byte(s))
	if err != nil {
		return nil, err
	}
	o, ok := v.(wire.Object)
	if !ok {
		return nil, fmt.Errorf("%s is not a JSON object", s)
	}
	return o, nil
}

// refuseRecorded fails a plan whose create or update would send a request the conformance
// suite recorded Stripe refusing (the record's rules), and reports whether it did. The request is
// the one the apply would send: the create's for a new object or a replacement, else the
// update's. It matches a refusal that it sends whole each member the refused request sent, a hash
// with the same members and a list the same elements in any order, so
// that reordering a list neither makes nor clears a refusal; on an update, the object must also
// hold at least what the refused probe's object was created with. A member the configuration
// does not know yet leaves the refusal to the plan at apply, which knows it. A /v1 request compares
// as its form sends it, a null or empty collection as the `""` a probe sent. A match is refused
// only on an account in the country the refusal was recorded in, which the provider reads then
// (AccountCountry): a refusal recorded on one country's account says nothing of another's, and
// an account the provider cannot read, as with a restricted key, is not refused.
func (r *Resource) refuseRecorded(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) bool {
	if len(r.m.refusals) == 0 {
		return false
	}
	var plan, config basetypes.ObjectValue
	diags := resp.Plan.Get(ctx, &plan)
	diags.Append(req.Config.Get(ctx, &config)...)
	if diags.HasError() {
		return false
	}
	update := !req.State.Raw.IsNull()
	var state basetypes.ObjectValue
	if update {
		if req.State.Get(ctx, &state).HasError() {
			return false
		}
		update = len(resp.RequiresReplace) == 0 && !r.replaces(plan, state, config)
	}
	var body, held wire.Object
	var err error
	if update {
		body, err = r.m.encodeUpdate(plan, state, req.Config.Raw)
		if err == nil {
			held, err = encodeObject(r.m.nodes, state.Attributes(), func(n *node) bool { return n.Create || n.Update })
			held = r.m.wrapCreate(held)
		}
	} else {
		body, err = r.m.encodeCreate(plan)
	}
	// What the encoder refuses, the apply reports.
	if err != nil {
		return false
	}
	// A /v1 body goes as a form, which sends a null, `[]` and `{}` all as the unset `""` that a
	// probe records, so the request compares as it is sent.
	if !r.m.v2 {
		body = wire.FormSent(body)
	}
	for _, x := range r.m.refusals {
		if x.Update != update || !wire.SendsAll(x.sends, body) || !wire.HoldsAll(x.holds, held) || !r.m.configKnown(req.Config.Raw, x.sends) || !r.country.in(ctx, x.AccountCountry) {
			continue
		}
		op := "create"
		if update {
			op = "update"
		}
		resp.Diagnostics.AddError("Stripe refuses this "+op,
			fmt.Sprintf("The plan is %s, which Stripe refused on a Stripe account in %s, as this provider's is, with %s (conformance suite probe `%s`). Change the configuration so that the %s sends something else.",
				r.m.describeRequest(update, x.sends, x.holds), x.AccountCountry, quoteMessage(x.Message), x.Probe, op))
		return true
	}
	return false
}

// planRefusalDoc words, for an argument's docs line, each request on it the plan refuses, with
// Stripe's message, the country of the account it holds in and the probe, or "" where there are
// none.
func (m *model) planRefusalDoc(n *node) string {
	var sentences []string
	for _, r := range n.PlanRefusals {
		sends, errSends := patternObject(r.Sends)
		holds, errHolds := patternObject(r.Holds)
		if errSends != nil || errHolds != nil {
			continue
		}
		sentences = append(sentences, fmt.Sprintf("Where the provider's Stripe account is in `%s`, the plan refuses %s, which Stripe refused there with %s (conformance suite probe `%s`).",
			r.AccountCountry, m.describeRequest(r.Update, sends, holds), quoteMessage(r.Message), r.Probe))
	}
	if len(sentences) == 0 {
		return ""
	}
	return strings.Join(sentences, " ") + " The provider reads the account's country only for a plan that sends such a request, and leaves the request to Stripe where the account is elsewhere or the key cannot read it."
}

// describeRequest words a refused request: the operation, what it sends and, on an update, what
// the object holds.
func (m *model) describeRequest(update bool, sends, holds wire.Object) string {
	op := "a create"
	if update {
		op = "an update"
	}
	out := op + " that sends " + describePattern(sends)
	if len(holds) > 0 {
		out += " to " + WithArticle(Noun(m.decl.TypeName)) + " that holds " + describePattern(holds)
	}
	return out
}

// quoteMessage quotes Stripe's message for prose, without the escapes %q would add.
func quoteMessage(s string) string { return `"` + s + `"` }

// configKnown reports whether the configuration knows every top-level argument a pattern names,
// whole: one that takes another resource's value before it exists is for the plan at apply.
func (m *model) configKnown(config tftypes.Value, pattern wire.Object) bool {
	for _, f := range pattern {
		found := false
		for _, n := range m.nodes {
			if n.Name != f.Name {
				continue
			}
			found = true
			v, _, err := tftypes.WalkAttributePath(config, tftypes.NewAttributePath().WithAttributeName(n.AttrName()))
			if err != nil {
				return false
			}
			if tv, ok := v.(tftypes.Value); !ok || !tv.IsFullyKnown() {
				return false
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// describePattern words a pattern as its leaves: `a.b = "v"`, `a.b` set where any value
// matches, and `[]` where a list's elements, in any order, hold the rest.
func describePattern(p wire.Object) string {
	var leaves []string
	var walk func(prefix string, v wire.Value)
	walk = func(prefix string, v wire.Value) {
		switch t := v.(type) {
		case wire.Object:
			if wire.IsAny(t) {
				leaves = append(leaves, "`"+prefix+"` set")
				return
			}
			if len(t) == 0 {
				leaves = append(leaves, "`"+prefix+" = {}`")
			}
			for _, f := range t {
				walk(prefix+"."+f.Name, f.Value)
			}
		case wire.Array:
			if len(t) == 0 {
				leaves = append(leaves, "`"+prefix+" = []`")
			}
			for _, e := range t {
				walk(prefix+"[]", e)
			}
		default:
			text, err := wire.EncodeJSON(t)
			if err != nil {
				text = []byte("?")
			}
			leaves = append(leaves, "`"+prefix+" = "+string(text)+"`")
		}
	}
	for _, f := range p {
		walk(f.Name, f.Value)
	}
	return joinAnd(leaves)
}
