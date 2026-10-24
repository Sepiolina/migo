package jsx

import (
	"bytes"
	"encoding/json"
	"math"
)

// Truthy is JavaScript truthiness for a value decoded from JSON.
func Truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0 && !math.IsNaN(x)
	}
	return true
}

// String is String(v) for a value decoded from JSON.
func String(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case float64:
		return NumToString(x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	}
	return "[object Object]"
}

// Stringify is JSON.stringify: no HTML escaping and no trailing newline.
func Stringify(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
