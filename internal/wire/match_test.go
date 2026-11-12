package wire

import "testing"

func mustObject(t *testing.T, s string) Object {
	t.Helper()
	v, err := DecodeJSON([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	o, ok := v.(Object)
	if !ok {
		t.Fatalf("%s is not an object", s)
	}
	return o
}

// A request sends a pattern where each member the pattern names is sent whole: a scalar equal,
// a hash with the same members, a list with the same elements in any order, and `$any` any
// value but null and the empty string. Members the pattern does not name are free.
func TestSendsAll(t *testing.T) {
	for _, tc := range []struct {
		name, pattern, body string
		want                bool
	}{
		{"equal scalar", `{"country":"GB"}`, `{"country":"GB","email":"a@b"}`, true},
		{"other scalar", `{"country":"GB"}`, `{"country":"FR"}`, false},
		{"absent member", `{"country":"GB"}`, `{"email":"a@b"}`, false},
		{"int and decimal", `{"score":20}`, `{"score":20}`, true},
		{"hash whole", `{"identity":{"country":"GB"}}`, `{"identity":{"country":"GB"}}`, true},
		{"hash with more members", `{"identity":{"country":"GB"}}`, `{"identity":{"country":"GB","entity_type":"company"}}`, false},
		{"hash with fewer members", `{"identity":{"country":"GB","entity_type":"company"}}`, `{"identity":{"country":"GB"}}`, false},
		{"any value", `{"url":{"$any":true}}`, `{"url":"https://example.com"}`, true},
		{"any is not null", `{"url":{"$any":true}}`, `{"url":null}`, false},
		{"any is not empty", `{"url":{"$any":true}}`, `{"url":""}`, false},
		{"the unset", `{"phases":""}`, `{"phases":""}`, true},
		{"any hash", `{"owner":{"$any":true}}`, `{"owner":{"email":"a@b"}}`, true},
		{"list in order", `{"phases":[{"price":"a"},{"price":"b"}]}`, `{"phases":[{"price":"a"},{"price":"b"}]}`, true},
		{"list reordered", `{"phases":[{"price":"a"},{"price":"b"}]}`, `{"phases":[{"price":"b"},{"price":"a"}]}`, true},
		{"list with another element", `{"phases":[{"price":"a"}]}`, `{"phases":[{"price":"a"},{"price":"b"}]}`, false},
		{"list element with more members", `{"phases":[{"price":"a"}]}`, `{"phases":[{"price":"a","start_date":1}]}`, false},
		{"one element for two", `{"phases":[{"price":{"$any":true}},{"price":"a"}]}`, `{"phases":[{"price":"a"},{"price":"c"}]}`, true},
		{"elements matched once", `{"phases":[{"price":"a"},{"price":"a"}]}`, `{"phases":[{"price":"a"},{"price":"b"}]}`, false},
		{"empty pattern", `{}`, `{"a":1}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := SendsAll(mustObject(t, tc.pattern), mustObject(t, tc.body)); got != tc.want {
				t.Errorf("SendsAll(%s, %s) = %t, want %t", tc.pattern, tc.body, got, tc.want)
			}
		})
	}
}

// An object holds a pattern where it holds at least each of its members: a hash may hold more
// members and a list more elements, matched in any order, each pattern element to its own.
func TestHoldsAll(t *testing.T) {
	for _, tc := range []struct {
		name, pattern, object string
		want                  bool
	}{
		{"nothing", `{}`, `{"a":1}`, true},
		{"nested value", `{"configuration":{"recipient":{"requested":true}}}`, `{"configuration":{"recipient":{"requested":true,"applied":true}},"dashboard":"none"}`, true},
		{"other value", `{"configuration":{"recipient":{"requested":true}}}`, `{"configuration":{"recipient":{"requested":false}}}`, false},
		{"absent", `{"dashboard":"none"}`, `{}`, false},
		{"any", `{"contact_email":{"$any":true}}`, `{"contact_email":"a@b"}`, true},
		{"any is not null", `{"contact_email":{"$any":true}}`, `{"contact_email":null}`, false},
		{"list holds an element", `{"discounts":[{"coupon":{"$any":true}}]}`, `{"discounts":[{"promotion_code":"p"},{"coupon":"c","discount":"d"}]}`, true},
		{"list lacks an element", `{"discounts":[{"coupon":{"$any":true}}]}`, `{"discounts":[{"promotion_code":"p"}]}`, false},
		{"list holds fewer", `{"discounts":[{"coupon":"a"},{"coupon":"b"}]}`, `{"discounts":[{"coupon":"a"}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := HoldsAll(mustObject(t, tc.pattern), mustObject(t, tc.object)); got != tc.want {
				t.Errorf("HoldsAll(%s, %s) = %t, want %t", tc.pattern, tc.object, got, tc.want)
			}
		})
	}
}

// A form body sends a null, an empty list and an empty hash, at any depth, as the empty string,
// and FormSent says so; anything else stays as it is.
func TestFormSent(t *testing.T) {
	body := Object{
		{Name: "phases", Value: Array{}},
		{Name: "description", Value: Null{}},
		{Name: "metadata", Value: Object{}},
		{Name: "items", Value: Array{Object{{Name: "price", Value: String("price_1")}, {Name: "tax_rates", Value: Array{}}}}},
		{Name: "quantity", Value: Int(2)},
	}
	got, err := EncodeJSON(FormSent(body))
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"phases":"","description":"","metadata":"","items":[{"price":"price_1","tax_rates":""}],"quantity":2}`; string(got) != want {
		t.Errorf("FormSent = %s, want %s", got, want)
	}
	if !SendsAll(mustObject(t, `{"phases":""}`), FormSent(body)) {
		t.Error("a recorded unset does not match the body as sent")
	}
}
