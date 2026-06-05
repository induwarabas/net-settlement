package settlement

import "github.com/shopspring/decimal"

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

type TradeResultStatus string

const TradeResultStatusFull = TradeResultStatus("FULL")
const TradeResultStatusPartial = TradeResultStatus("PARTIAL")
const TradeResultStatusDeferred = TradeResultStatus("DEFERRED")

type TradeResult struct {
	Trade                 Trade
	Status                TradeResultStatus
	SettledQuantity       decimal.Decimal
	SettledQuoteQuantity  decimal.Decimal
	DeferredQuantity      decimal.Decimal
	DeferredQuoteQuantity decimal.Decimal
}

type InstructionDirection string

// Remove assets from ledger
const InstructionDirectionDebit = InstructionDirection("DEBIT")

// Add assets to ledger
const InstructionDirectionCredit = InstructionDirection("CREDIT")

type Instruction struct {
	Member         string
	Asset          string
	OpeningBalance decimal.Decimal
	NetAmount      decimal.Decimal
	ClosingBalance decimal.Decimal
	Direction      InstructionDirection
}

type Result struct {
	Instructions []*Instruction
	Trades       []*TradeResult
}

type Results struct {
	Batches  []*Result
	Deferred []Trade
}

type LedgerEntry interface {
	Member() string
	Asset() string
	Balance() decimal.Decimal
}

type Asset interface {
	Symbol() string
	DustThreshold() decimal.Decimal
	Precision() int
}

// GenerateInstructions runs the settlement engine over the provided trades and
// ledger balances and returns settlement instructions grouped into independent
// batches, along with any trades that could not be settled (deferred).
//
// When strictFifo is true, an unsettled trade blocks all later trades
// for the same member-asset pair from settling until the earlier one clears.
func GenerateInstructions(trades []Trade, ledger []LedgerEntry, assets []Asset, strictFifo bool) Results {
	eng := newEngine(strictFifo)
	eng.init(trades, ledger, assets)
	return eng.run()
}
