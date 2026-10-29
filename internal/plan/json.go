package plan

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// decodeJSON preserves exact identifiers in generic results and page context.
func decodeJSON(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err != nil {
			return err
		}
		return errors.New("multiple JSON values")
	}
	return nil
}
