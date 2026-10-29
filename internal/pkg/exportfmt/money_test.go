package exportfmt

import (
	"encoding/json"
	"testing"
)

func TestMoneyPreservesExactJSONCents(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  string
	}{
		{int64(1990), "19.90"},
		{float64(1990), "19.90"},
		{"1990", "19.90"},
		{json.Number("1990"), "19.90"},
		{json.Number("9007199254740993"), "90071992547409.93"},
		{json.Number("9223372036854775807"), "92233720368547758.07"},
	} {
		if got := Money(tc.value); got != tc.want {
			t.Errorf("Money(%v) = %v, want %s", tc.value, got, tc.want)
		}
	}
}
