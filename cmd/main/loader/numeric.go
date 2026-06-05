package loader

import (
	"strings"

	"github.com/shopspring/decimal"
)

// strToDecimal parses a CSV numeric cell into a decimal.Decimal, stripping
// thousands separators and surrounding whitespace. Panics on unparseable input.
func strToDecimal(s string) decimal.Decimal {
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimSpace(s)
	d := decimal.RequireFromString(s)
	return d
}
