// Package wire is the closed JSON value type the engine moves data through, with the
// form and JSON codecs for Stripe request bodies and the JSON decoder for responses.
// No part of it uses untyped values: every value is one of the types below.
package wire

// Value is a JSON value. The set of implementations is closed: Null, Bool, Int, String,
// Decimal, Array and Object.
type Value interface {
	isValue()
}

// Null is JSON null. In a form body it encodes as an empty value, which is how Stripe
// unsets a field.
type Null struct{}

// Bool is a JSON boolean.
type Bool bool

// Int is a JSON number that is an integer in the int64 range (amounts, timestamps).
type Int int64

// String is a JSON string. Stripe's `*_decimal` fields arrive as strings and stay here.
type String string

// Decimal is a JSON number that is not an int64, kept as its literal text so no
// precision is lost.
type Decimal string

// Array is a JSON array.
type Array []Value

// Field is one member of an Object.
type Field struct {
	Name  string
	Value Value
}

// Object is a JSON object that keeps member order, so encoded bodies are deterministic.
type Object []Field

func (Null) isValue()    {}
func (Bool) isValue()    {}
func (Int) isValue()     {}
func (String) isValue()  {}
func (Decimal) isValue() {}
func (Array) isValue()   {}
func (Object) isValue()  {}

// Get returns the first member with the given name.
func (o Object) Get(name string) (Value, bool) {
	for _, f := range o {
		if f.Name == name {
			return f.Value, true
		}
	}
	return nil, false
}

// GetString returns the member as a string when it is present and a String.
func (o Object) GetString(name string) (string, bool) {
	v, ok := o.Get(name)
	if !ok {
		return "", false
	}
	s, ok := v.(String)
	return string(s), ok
}

// GetObject returns the member as an Object when it is present and one.
func (o Object) GetObject(name string) (Object, bool) {
	v, ok := o.Get(name)
	if !ok {
		return nil, false
	}
	obj, ok := v.(Object)
	return obj, ok
}

// GetBool returns the member as a bool when it is present and a Bool.
func (o Object) GetBool(name string) (value, ok bool) {
	v, found := o.Get(name)
	if !found {
		return false, false
	}
	b, isBool := v.(Bool)
	return bool(b), isBool
}
