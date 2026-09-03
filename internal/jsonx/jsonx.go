// Package jsonx provides strict decoding helpers for dynamic JSON values.
//
// Dynamic JSON is decoded with json.Number so numeric tokens are not
// converted to float64 before the caller can inspect or marshal them again.
package jsonx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ErrTrailingData reports non-whitespace input after the first JSON value.
var ErrTrailingData = errors.New("trailing data after JSON value")

// Unmarshal decodes exactly one JSON value into v while preserving numbers as
// json.Number. Whitespace after the value is accepted, but any second value
// (or other non-whitespace trailing input) is rejected.
func Unmarshal(data []byte, v any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(v); err != nil {
		// Keep the established json.Unmarshal diagnostic for truncated input;
		// switching to Decoder is an implementation detail needed by UseNumber.
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return errors.New("unexpected end of JSON input")
		}
		return err
	}

	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err == io.EOF {
		return nil
	} else if err == nil {
		return fmt.Errorf("%w: multiple JSON values", ErrTrailingData)
	} else {
		return fmt.Errorf("%w: %v", ErrTrailingData, err)
	}
}

// Decode decodes exactly one dynamic JSON value while preserving numbers as
// json.Number. It is equivalent to Unmarshal(data, &value).
func Decode(data []byte) (any, error) {
	var value any
	if err := Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return value, nil
}
