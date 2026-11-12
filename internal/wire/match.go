package wire

import "strconv"

// AnyName is the member name of the object that stands, in a request pattern, for any value
// the request or the object holds: `{"$any":true}`.
const AnyName = "$any"

// Any is the pattern value that matches any value but null and the empty string: it stands for
// a value the conformance suite generated, which is never empty, and on /v1 the empty string is
// the unset.
var Any = Object{{Name: AnyName, Value: Bool(true)}}

// IsAny reports whether a pattern value is Any.
func IsAny(v Value) bool {
	o, ok := v.(Object)
	return ok && len(o) == 1 && o[0].Name == AnyName
}

// SendsAll reports whether a request body sends what a pattern sends: each member of the
// pattern is a member of the body, equal to it whole. A hash equals one with the same members,
// each equal; a list equals one with as many elements, each equal to a different element of the
// other, in any order, so that a reorder neither makes nor breaks a match. Members of the body
// the pattern does not name are not compared.
func SendsAll(pattern, body Object) bool {
	for _, f := range pattern {
		v, ok := body.Get(f.Name)
		if !ok || !sentEqual(f.Value, v) {
			return false
		}
	}
	return true
}

// HoldsAll reports whether an object holds at least what a pattern holds: each member of the
// pattern is a member of the object that holds it in turn, a hash at least the pattern's
// members and a list, in any order, an element holding each of the pattern's elements, a
// different one for each.
func HoldsAll(pattern, object Object) bool {
	return held(pattern, object)
}

func sentEqual(p, v Value) bool {
	if IsAny(p) {
		return present(v)
	}
	switch p := p.(type) {
	case Object:
		o, ok := v.(Object)
		if !ok || len(o) != len(p) {
			return false
		}
		return SendsAll(p, o)
	case Array:
		a, ok := v.(Array)
		return ok && len(a) == len(p) && matched(p, a, sentEqual)
	}
	return scalarEqual(p, v)
}

func held(p, v Value) bool {
	if IsAny(p) {
		return present(v)
	}
	switch p := p.(type) {
	case Object:
		o, ok := v.(Object)
		if !ok {
			return false
		}
		for _, f := range p {
			if inner, ok := o.Get(f.Name); !ok || !held(f.Value, inner) {
				return false
			}
		}
		return true
	case Array:
		a, ok := v.(Array)
		return ok && len(a) >= len(p) && matched(p, a, held)
	}
	return scalarEqual(p, v)
}

// matched reports whether each pattern element matches a different element of a, by eq.
func matched(p, a Array, eq func(p, v Value) bool) bool {
	used := make([]bool, len(a))
	var assign func(i int) bool
	assign = func(i int) bool {
		if i == len(p) {
			return true
		}
		for j := range a {
			if !used[j] && eq(p[i], a[j]) {
				used[j] = true
				if assign(i + 1) {
					return true
				}
				used[j] = false
			}
		}
		return false
	}
	return assign(0)
}

func present(v Value) bool {
	_, null := v.(Null)
	return v != nil && !null && v != String("")
}

// scalarEqual compares two scalars, an Int and a Decimal by their text.
func scalarEqual(p, v Value) bool {
	switch p := p.(type) {
	case Null, Bool, String:
		return p == v
	case Int:
		switch v := v.(type) {
		case Int:
			return p == v
		case Decimal:
			return strconv.FormatInt(int64(p), 10) == string(v)
		}
	case Decimal:
		switch v := v.(type) {
		case Decimal:
			return p == v
		case Int:
			return string(p) == strconv.FormatInt(int64(v), 10)
		}
	}
	return false
}
