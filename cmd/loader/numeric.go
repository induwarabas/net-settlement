package loader

import (
	"strings"

	"github.com/shopspring/decimal"
)

func strToDecimal(s string) decimal.Decimal {
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimSpace(s)
	d := decimal.RequireFromString(s)
	return d
}
