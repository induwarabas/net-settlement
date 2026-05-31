package settlement

import (
	"math/big"

	"github.com/shopspring/decimal"
)

const Places = 6

var Scale = big.NewInt(1_000_000)
var ScaleDecimal = decimal.New(1, Places)

// multiply multiplies two scaled big.Ints and re-normalises by Scale.
func multiply(a, b *big.Int) *big.Int {
	r := new(big.Int).Mul(a, b)
	r.Quo(r, Scale)
	return r
}

// multiplyAndDivide computes a × num / denom where all three are scaled values.
func multiplyAndDivide(a, num, denom *big.Int) *big.Int {
	r := new(big.Int).Mul(a, num)
	r.Quo(r, denom)
	return r
}

func fromDecimal(value decimal.Decimal) *big.Int {
	return value.Mul(ScaleDecimal).BigInt()
}

func toDecimal(value *big.Int) decimal.Decimal {
	return decimal.NewFromBigInt(value, 0).Div(ScaleDecimal)
}
