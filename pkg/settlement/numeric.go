package settlement

import (
	"math/big"

	"github.com/shopspring/decimal"
)

const Places = 18

var Scale = new(big.Int).Exp(big.NewInt(10), big.NewInt(Places+2), nil)
var ScaleDecimal = decimal.New(1, Places+2)

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
