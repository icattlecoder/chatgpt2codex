package tool

import (
	"bytes"
	"encoding/json"
)

func decodeJSON(raw json.RawMessage, dst any, disallowUnknown bool) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return ValidationError("request body is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if disallowUnknown {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(dst); err != nil {
		return ValidationError("invalid JSON body: " + err.Error())
	}
	return nil
}
