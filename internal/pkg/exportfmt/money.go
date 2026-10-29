// Package exportfmt contains presentation shared by synchronous and queued exports.
package exportfmt

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// Money formats integer cents without converting exact JSON numbers to floats.
func Money(value any) any {
	var cents int64
	switch n := value.(type) {
	case int:
		cents = int64(n)
	case int32:
		cents = int64(n)
	case int64:
		cents = n
	case float64:
		cents = int64(n)
	case json.Number:
		parsed, err := n.Int64()
		if err != nil {
			return value
		}
		cents = parsed
	case string:
		parsed, err := strconv.ParseInt(n, 10, 64)
		if err != nil {
			return value
		}
		cents = parsed
	default:
		return value
	}
	part := cents % 100
	if part < 0 {
		part = -part
	}
	return fmt.Sprintf("%d.%02d", cents/100, part)
}
