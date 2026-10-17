package wire

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// EncodeForm renders an Object as a Stripe form body using bracket notation:
// `a[b][0][c]=v`. Arrays use explicit indexes. Null, an empty array and an empty object
// all encode as an empty value, which is how Stripe unsets a field. Members keep their
// order.
func EncodeForm(obj Object) (string, error) {
	var pairs []string
	for _, f := range obj {
		var err error
		if pairs, err = appendForm(pairs, url.QueryEscape(f.Name), f.Value); err != nil {
			return "", err
		}
	}
	return strings.Join(pairs, "&"), nil
}

func appendForm(pairs []string, key string, v Value) ([]string, error) {
	scalar := func(s string) []string { return append(pairs, key+"="+url.QueryEscape(s)) }
	switch t := v.(type) {
	case Null:
		return scalar(""), nil
	case Bool:
		return scalar(strconv.FormatBool(bool(t))), nil
	case Int:
		return scalar(strconv.FormatInt(int64(t), 10)), nil
	case Decimal:
		return scalar(string(t)), nil
	case String:
		return scalar(string(t)), nil
	case Array:
		if len(t) == 0 {
			return scalar(""), nil
		}
		for i, item := range t {
			var err error
			if pairs, err = appendForm(pairs, key+"%5B"+strconv.Itoa(i)+"%5D", item); err != nil {
				return nil, err
			}
		}
		return pairs, nil
	case Object:
		if len(t) == 0 {
			return scalar(""), nil
		}
		for _, f := range t {
			var err error
			if pairs, err = appendForm(pairs, key+"%5B"+url.QueryEscape(f.Name)+"%5D", f.Value); err != nil {
				return nil, err
			}
		}
		return pairs, nil
	default:
		return nil, fmt.Errorf("wire: unknown value type %T", v)
	}
}
