package settlement

import (
	"math/big"

	"github.com/shopspring/decimal"
)

// Places is the number of decimal places preserved when converting an engine
// big.Int back to a decimal.Decimal. scaleDigits adds two extra digits of
// headroom so intermediate multiplications and divisions don't lose precision
// at the Places boundary.
const Places = 18
const scaleDigits = Places + 2

// Scale is the fixed-point multiplier (10^scaleDigits) applied to every value
// inside the engine; ScaleDecimal is the same factor expressed as a
// decimal.Decimal for boundary conversions.
var (
	Scale        = new(big.Int).Exp(big.NewInt(10), big.NewInt(scaleDigits), nil)
	ScaleDecimal = decimal.New(1, scaleDigits)
)

type roundingMode int

const (
	roundDown roundingMode = iota
	roundUp
)

// precisionStep returns the scaled big.Int representing one unit at the given
// asset precision (e.g. precision=2 -> 10^(scaleDigits-2)). Values that are
// multiples of this step have no digits beyond the asset's precision.
func precisionStep(precision int) *big.Int {
	if precision >= scaleDigits {
		return big.NewInt(1)
	}
	if precision < 0 {
		precision = 0
	}
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scaleDigits-precision)), nil)
}

// roundToPrecision snaps a non-negative scaled value to the asset's precision
// using the requested rounding mode.
func roundToPrecision(value *big.Int, precision int, mode roundingMode) *big.Int {
	if value.Sign() == 0 {
		return new(big.Int)
	}
	step := precisionStep(precision)
	if step.Cmp(big.NewInt(1)) == 0 {
		return new(big.Int).Set(value)
	}
	q, r := new(big.Int).QuoRem(value, step, new(big.Int))
	if mode == roundUp && r.Sign() != 0 {
		q.Add(q, big.NewInt(1))
	}
	return q.Mul(q, step)
}

// isBelowDust reports whether value is strictly positive but smaller than dust.
// Exactly zero is not considered dust.
func isBelowDust(value, dust *big.Int) bool {
	if value.Sign() <= 0 {
		return false
	}
	return value.Cmp(dust) < 0
}

// multiply multiplies two scaled big.Ints and renormalises by Scale so the
// result remains in the same fixed-point representation.
func multiply(a, b *big.Int) *big.Int {
	r := new(big.Int).Mul(a, b)
	r.Quo(r, Scale)
	return r
}

// multiplyAndDivide computes a × num / denom where all values are in the same
// scaled representation. Used to derive a proportional quote (or base) quantity
// when partially reversing a trade.
func multiplyAndDivide(a, num, denom *big.Int) *big.Int {
	r := new(big.Int).Mul(a, num)
	r.Quo(r, denom)
	return r
}

// fromDecimal converts a decimal.Decimal to a scaled big.Int (× 1_000_000).
func fromDecimal(value decimal.Decimal) *big.Int {
	return value.Mul(ScaleDecimal).BigInt()
}

// toDecimal converts a scaled big.Int back to a decimal.Decimal (÷ 1_000_000).
func toDecimal(value *big.Int) decimal.Decimal {
	return decimal.NewFromBigInt(value, 0).Div(ScaleDecimal).Round(Places)
}
