package wire

import (
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// shapes covers each row of the ADR's nested-object table.
var shapes = []struct {
	name string
	body Object
	form string
	json string
}{
	{
		name: "scalars",
		body: Object{{"name", String("Gold plan")}, {"active", Bool(true)}, {"unit_amount", Int(1200)}},
		form: "name=Gold+plan&active=true&unit_amount=1200",
		json: `{"name":"Gold plan","active":true,"unit_amount":1200}`,
	},
	{
		name: "hash with fixed keys",
		body: Object{{"recurring", Object{{"interval", String("month")}, {"interval_count", Int(1)}}}},
		form: "recurring%5Binterval%5D=month&recurring%5Binterval_count%5D=1",
		json: `{"recurring":{"interval":"month","interval_count":1}}`,
	},
	{
		name: "array of scalars",
		body: Object{{"enabled_events", Array{String("charge.succeeded"), String("*")}}},
		form: "enabled_events%5B0%5D=charge.succeeded&enabled_events%5B1%5D=%2A",
		json: `{"enabled_events":["charge.succeeded","*"]}`,
	},
	{
		name: "array of hashes",
		body: Object{{"tiers", Array{
			Object{{"up_to", Int(10)}, {"unit_amount", Int(5)}},
			Object{{"up_to", String("inf")}, {"unit_amount", Int(3)}},
		}}},
		form: "tiers%5B0%5D%5Bup_to%5D=10&tiers%5B0%5D%5Bunit_amount%5D=5&tiers%5B1%5D%5Bup_to%5D=inf&tiers%5B1%5D%5Bunit_amount%5D=3",
		json: `{"tiers":[{"up_to":10,"unit_amount":5},{"up_to":"inf","unit_amount":3}]}`,
	},
	{
		name: "hash keyed by a variable",
		body: Object{{"currency_options", Object{
			{"eur", Object{{"unit_amount", Int(1100)}}},
			{"gbp", Object{{"unit_amount", Int(900)}}},
		}}},
		form: "currency_options%5Beur%5D%5Bunit_amount%5D=1100&currency_options%5Bgbp%5D%5Bunit_amount%5D=900",
		json: `{"currency_options":{"eur":{"unit_amount":1100},"gbp":{"unit_amount":900}}}`,
	},
	{
		name: "free-form map with unset keys",
		body: Object{{"metadata", Object{{"team", String("a/b c")}, {"old", Null{}}}}},
		form: "metadata%5Bteam%5D=a%2Fb+c&metadata%5Bold%5D=",
		json: `{"metadata":{"team":"a/b c","old":null}}`,
	},
	{
		name: "empty containers unset in a form",
		body: Object{{"tiers", Array{}}, {"metadata", Object{}}},
		form: "tiers=&metadata=",
		json: `{"tiers":[],"metadata":{}}`,
	},
	{
		name: "decimal",
		body: Object{{"ratio", Decimal("0.125")}, {"big", Decimal("92233720368547758070")}},
		form: "ratio=0.125&big=92233720368547758070",
		json: `{"ratio":0.125,"big":92233720368547758070}`,
	},
}

func TestJSONRoundTrip(t *testing.T) {
	for _, tc := range shapes {
		t.Run(tc.name, func(t *testing.T) {
			got, err := EncodeJSON(tc.body)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.json {
				t.Fatalf("encoded %s, want %s", got, tc.json)
			}
			back, err := DecodeJSON(got)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(back, Value(tc.body)) {
				t.Fatalf("round trip gave %#v, want %#v", back, tc.body)
			}
		})
	}
}

// parseForm rebuilds a tree of strings from a bracket form body.
func parseForm(t *testing.T, body string) Object {
	t.Helper()
	var root Object
	for _, pair := range strings.Split(body, "&") {
		rawKey, rawValue, _ := strings.Cut(pair, "=")
		key, err := url.QueryUnescape(rawKey)
		if err != nil {
			t.Fatal(err)
		}
		value, err := url.QueryUnescape(rawValue)
		if err != nil {
			t.Fatal(err)
		}
		name, rest, _ := strings.Cut(key, "[")
		var path []string
		for rest != "" {
			seg, after, _ := strings.Cut(rest, "]")
			path = append(path, seg)
			rest = strings.TrimPrefix(after, "[")
		}
		root = setPath(root, name, path, value)
	}
	return root
}

func setPath(obj Object, name string, path []string, value string) Object {
	for i := range obj {
		if obj[i].Name == name {
			obj[i].Value = place(obj[i].Value, path, value)
			return obj
		}
	}
	return append(obj, Field{Name: name, Value: place(nil, path, value)})
}

func place(existing Value, path []string, value string) Value {
	if len(path) == 0 {
		return String(value)
	}
	if _, err := strconv.Atoi(path[0]); err == nil {
		arr, _ := existing.(Array)
		i, _ := strconv.Atoi(path[0])
		for len(arr) <= i {
			arr = append(arr, nil)
		}
		arr[i] = place(arr[i], path[1:], value)
		return arr
	}
	obj, _ := existing.(Object)
	return setPath(obj, path[0], path[1:], value)
}

// stringify is the form-wire view of a body: every scalar becomes its text, and unset
// values become empty strings.
func stringify(v Value) Value {
	switch t := v.(type) {
	case Null:
		return String("")
	case Bool:
		return String(strconv.FormatBool(bool(t)))
	case Int:
		return String(strconv.FormatInt(int64(t), 10))
	case Decimal:
		return String(string(t))
	case Array:
		if len(t) == 0 {
			return String("")
		}
		out := make(Array, len(t))
		for i, item := range t {
			out[i] = stringify(item)
		}
		return out
	case Object:
		if len(t) == 0 {
			return String("")
		}
		out := make(Object, len(t))
		for i, f := range t {
			out[i] = Field{f.Name, stringify(f.Value)}
		}
		return out
	default:
		return v
	}
}

func TestFormEncodeAndParseBack(t *testing.T) {
	for _, tc := range shapes {
		t.Run(tc.name, func(t *testing.T) {
			got, err := EncodeForm(tc.body)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.form {
				t.Fatalf("encoded %s, want %s", got, tc.form)
			}
			if back, want := parseForm(t, got), stringify(tc.body); !reflect.DeepEqual(Value(back), want) {
				t.Fatalf("parsed %#v, want %#v", back, want)
			}
		})
	}
}

func TestDecodeJSONNumbers(t *testing.T) {
	v, err := DecodeJSON([]byte(`[1, -2, 1.5, 1e3, 9223372036854775808]`))
	if err != nil {
		t.Fatal(err)
	}
	want := Array{Int(1), Int(-2), Decimal("1.5"), Decimal("1e3"), Decimal("9223372036854775808")}
	if !reflect.DeepEqual(v, Value(want)) {
		t.Fatalf("got %#v, want %#v", v, want)
	}
}

func TestDecodeJSONRejectsMalformed(t *testing.T) {
	for _, in := range []string{``, `{`, `{"a":1} x`, `[1,]`, `{"a" 1}`} {
		if _, err := DecodeJSON([]byte(in)); err == nil {
			t.Errorf("%q: expected an error", in)
		}
	}
}

func TestEncodeJSONRejectsBadDecimal(t *testing.T) {
	for _, in := range []Decimal{"", "abc", "true", `"1"`, "1 2"} {
		if _, err := EncodeJSON(in); err == nil {
			t.Errorf("%q: expected an error", string(in))
		}
	}
}

func TestObjectAccessors(t *testing.T) {
	o := Object{{"a", String("x")}, {"b", Object{{"c", Bool(true)}}}}
	if s, ok := o.GetString("a"); !ok || s != "x" {
		t.Fatal("GetString")
	}
	if _, ok := o.GetString("b"); ok {
		t.Fatal("GetString on an object")
	}
	inner, ok := o.GetObject("b")
	if !ok {
		t.Fatal("GetObject")
	}
	if b, ok := inner.GetBool("c"); !ok || !b {
		t.Fatal("GetBool")
	}
}
