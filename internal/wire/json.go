package wire

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// DecodeJSON parses one JSON document into a Value. Numbers that fit int64 become Int and
// every other number becomes a Decimal holding its literal text.
func DecodeJSON(data []byte) (Value, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("wire: unexpected data after the JSON document")
	}
	return v, nil
}

func decodeValue(dec *json.Decoder) (Value, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("wire: %w", err)
	}
	switch t := tok.(type) {
	case nil:
		return Null{}, nil
	case bool:
		return Bool(t), nil
	case string:
		return String(t), nil
	case json.Number:
		if n, err := strconv.ParseInt(string(t), 10, 64); err == nil {
			return Int(n), nil
		}
		return Decimal(t), nil
	case json.Delim:
		switch t {
		case '[':
			arr := Array{}
			for dec.More() {
				item, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, item)
			}
			if _, err := dec.Token(); err != nil {
				return nil, fmt.Errorf("wire: %w", err)
			}
			return arr, nil
		case '{':
			obj := Object{}
			for dec.More() {
				key, err := dec.Token()
				if err != nil {
					return nil, fmt.Errorf("wire: %w", err)
				}
				name, ok := key.(string)
				if !ok {
					return nil, errors.New("wire: object key is not a string")
				}
				item, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				obj = append(obj, Field{Name: name, Value: item})
			}
			if _, err := dec.Token(); err != nil {
				return nil, fmt.Errorf("wire: %w", err)
			}
			return obj, nil
		}
	}
	return nil, fmt.Errorf("wire: unexpected JSON token %v", tok)
}

// EncodeJSON renders a Value as JSON, keeping object member order.
func EncodeJSON(v Value) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeJSON(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeJSON(buf *bytes.Buffer, v Value) error {
	switch t := v.(type) {
	case Null:
		buf.WriteString("null")
	case Bool:
		buf.WriteString(strconv.FormatBool(bool(t)))
	case Int:
		buf.WriteString(strconv.FormatInt(int64(t), 10))
	case Decimal:
		if len(t) == 0 || (t[0] != '-' && (t[0] < '0' || t[0] > '9')) || !json.Valid([]byte(t)) {
			return fmt.Errorf("wire: %q is not a JSON number", string(t))
		}
		buf.WriteString(string(t))
	case String:
		quoted, err := json.Marshal(string(t))
		if err != nil {
			return fmt.Errorf("wire: %w", err)
		}
		buf.Write(quoted)
	case Array:
		buf.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeJSON(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case Object:
		buf.WriteByte('{')
		for i, f := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			name, err := json.Marshal(f.Name)
			if err != nil {
				return fmt.Errorf("wire: %w", err)
			}
			buf.Write(name)
			buf.WriteByte(':')
			if err := writeJSON(buf, f.Value); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		return fmt.Errorf("wire: unknown value type %T", v)
	}
	return nil
}
