// Package settlement implements a multilateral trade settlement engine.
//
// Given a set of trades, member ledger balances, and asset reference data, it
// produces settlement instructions (debits/credits) and classifies each trade
// as fully settled, partially settled, or deferred. Per-asset precision and
// dust thresholds are honoured throughout.
package settlement

import "github.com/shopspring/decimal"

// Trade is the read-only view the engine needs of an executed trade.
// ExecTime is reported in nanoseconds since the Unix epoch.
type Trade interface {
	TradeID() string
	ExecTime() int64
	Buyer() string
	Seller() string
	BaseAsset() string
	QuoteAsset() string
	Quantity() decimal.Decimal
	Price() decimal.Decimal
}

// TradeResultStatus indicates how much of a trade settled after the engine ran.
type TradeResultStatus string

const (
	// TradeResultStatusFull means the trade settles in full.
	TradeResultStatusFull = TradeResultStatus("FULL")
	// TradeResultStatusPartial means part of the trade settles and the
	// remainder is deferred. Both portions clear per-asset dust thresholds.
	TradeResultStatusPartial = TradeResultStatus("PARTIAL")
	// TradeResultStatusDeferred means none of the trade settles in this run.
	TradeResultStatusDeferred = TradeResultStatus("DEFERRED")
)

// TradeResult is the engine's verdict on a single trade. Settled + Deferred
// quantities sum to the original order amounts on both base and quote sides.
type TradeResult struct {
	Trade                 Trade
	Status                TradeResultStatus
	SettledQuantity       decimal.Decimal
	SettledQuoteQuantity  decimal.Decimal
	DeferredQuantity      decimal.Decimal
	DeferredQuoteQuantity decimal.Decimal
}

// InstructionDirection is the direction of a settlement instruction relative to
// the member's ledger.
type InstructionDirection string

const (
	// InstructionDirection_Out removes assets from the member's ledger.
	InstructionDirection_Out = InstructionDirection("OUT")
	// InstructionDirection_In adds assets to the member's ledger.
	InstructionDirection_In = InstructionDirection("IN")
)

// Instruction is a single ledger movement: one member, one asset, one
// direction. NetAmount is the absolute amount to move; OpeningBalance and
// ClosingBalance bracket the movement.
type Instruction struct {
	Member         string
	Asset          string
	OpeningBalance decimal.Decimal
	NetAmount      decimal.Decimal
	ClosingBalance decimal.Decimal
	Direction      InstructionDirection
}

// Result groups the trades and instructions belonging to a single independent
// settlement batch. Batches share no member-asset overlap and can be settled
// atomically and independently.
type Result struct {
	Instructions []*Instruction
	Trades       []*TradeResult
}

// Results is the full output of a settlement run: independent batches plus the
// list of trades that could not be settled at all.
type Results struct {
	Batches  []*Result
	Deferred []Trade
}

// LedgerEntry is the read-only view the engine needs of a single member-asset
// opening balance.
type LedgerEntry interface {
	Member() string
	Asset() string
	Balance() decimal.Decimal
}

// Asset is the read-only view the engine needs of an asset's reference data.
// Precision is the number of decimal places the asset supports; DustThreshold
// is the smallest amount considered economically meaningful for that asset.
type Asset interface {
	Symbol() string
	DustThreshold() decimal.Decimal
	Precision() int
}

// GenerateInstructions runs the settlement engine over the provided trades,
// ledger balances and asset reference data, and returns settlement instructions
// grouped into independent batches along with any trades that could not be
// settled (deferred).
//
// When strictFifo is true, an unsettled trade blocks all later trades for the
// same member-asset pair from settling until the earlier one clears.
//
// All amounts are rounded to each asset's declared precision; sub-dust
// residuals are eliminated by either fully reversing the trade or expanding
// the reversal until both sides clear dust.
func GenerateInstructions(trades []Trade, ledger []LedgerEntry, assets []Asset, strictFifo bool) Results {
	eng := newEngine(strictFifo)
	eng.init(trades, ledger, assets)
	return eng.run()
}
