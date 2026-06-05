package settlement

import (
	"encoding/csv"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"sort"
	"time"

	"github.com/shopspring/decimal"
)

type trade struct {
	TradeId           int
	Buyer             int
	Seller            int
	BaseAsset         int
	QuoteAsset        int
	BaseQuantity      *big.Int
	Price             *big.Int
	QuoteQuantity     *big.Int
	RemainingBaseQty  *big.Int
	RemainingQuoteQty *big.Int
}

type assetLiability struct {
	Start  int
	Trades []*trade
}

type location struct {
	member int
	asset  int
}

func newLocation(member int, asset int) *location {
	return &location{
		member: member,
		asset:  asset,
	}
}

type engine struct {
	ledger            [][]*big.Int
	netting           [][]*big.Int
	trades            []*trade
	tradeIndex        map[string]int
	assetIndex        map[string]int
	memberIndex       map[string]int
	assets            []Asset
	members           []string
	assetLiability    [][]*assetLiability
	negativeLocations []*location
	originalTrades    []Trade
	strictFifo        bool
}

// newEngine creates an empty settlement engine. Call init before run.
func newEngine(strictFifo bool) *engine {
	m := &engine{
		ledger:         make([][]*big.Int, 0),
		netting:        make([][]*big.Int, 0),
		trades:         make([]*trade, 0),
		tradeIndex:     make(map[string]int),
		assetIndex:     make(map[string]int),
		memberIndex:    make(map[string]int),
		originalTrades: make([]Trade, 0),
		strictFifo:     strictFifo,
	}
	return m
}

// memberId returns the integer ID for a member name, creating one if needed.
func (m *engine) memberId(member string) int {
	id, ok := m.memberIndex[member]
	if !ok {
		id = len(m.memberIndex)
		m.memberIndex[member] = id
	}
	return id
}

// assetId returns the integer ID for an asset symbol, creating one if needed.
func (m *engine) assetId(asset string) int {
	id, ok := m.assetIndex[asset]
	if !ok {
		id = len(m.assetIndex)
		m.assetIndex[asset] = id
	}
	return id
}

// init sorts trades by execution time, indexes all members and assets, seeds the
// netting matrix from ledger balances, and builds per-member-asset liability
// lists that the iterative resolver will walk in reverse (newest-first) order.
func (m *engine) init(trades []Trade, ledger []LedgerEntry, assets []Asset) error {
	slog.Info("Initializing settlement engine")
	sort.SliceStable(trades, func(i, j int) bool {
		return trades[i].ExecTime() < trades[j].ExecTime()
	})
	assetMap := make(map[string]Asset)
	for _, asset := range assets {
		assetMap[asset.Symbol()] = asset
	}
	for i, trd := range trades {
		m.originalTrades = append(m.originalTrades, trd)

		quantity := fromDecimal(trd.Quantity())
		price := fromDecimal(trd.Price())
		quoteQty := multiply(quantity, price)
		m.trades = append(m.trades, &trade{
			TradeId:           i,
			Buyer:             m.memberId(trd.Buyer()),
			Seller:            m.memberId(trd.Seller()),
			BaseAsset:         m.assetId(trd.BaseAsset()),
			QuoteAsset:        m.assetId(trd.QuoteAsset()),
			BaseQuantity:      new(big.Int).Set(quantity),
			Price:             new(big.Int).Set(price),
			QuoteQuantity:     new(big.Int).Set(quoteQty),
			RemainingBaseQty:  new(big.Int).Set(quantity),
			RemainingQuoteQty: new(big.Int).Set(quoteQty),
		})
	}

	m.ledger = make([][]*big.Int, len(m.memberIndex))
	m.netting = make([][]*big.Int, len(m.memberIndex))

	for i := 0; i < len(m.memberIndex); i++ {
		m.ledger[i] = make([]*big.Int, len(m.assetIndex))
		m.netting[i] = make([]*big.Int, len(m.assetIndex))
		for j := 0; j < len(m.assetIndex); j++ {
			m.ledger[i][j] = new(big.Int)
			m.netting[i][j] = new(big.Int)
		}
	}

	for _, entry := range ledger {
		memberId, ok := m.memberIndex[entry.Member()]
		if !ok {
			continue
		}
		assetId, k := m.assetIndex[entry.Asset()]
		if !k {
			continue
		}
		balance := fromDecimal(entry.Balance())
		m.ledger[memberId][assetId].Set(balance)
		m.netting[memberId][assetId].Set(balance)
	}
	m.members = make([]string, len(m.memberIndex))
	for member, id := range m.memberIndex {
		m.members[id] = member
	}

	m.assets = make([]Asset, len(m.assetIndex))
	for asset, id := range m.assetIndex {
		ast := assetMap[asset]
		if ast == nil {
			fmt.Println("Asset reference data missing: ", asset, " (", id, ")")
			return fmt.Errorf("asset reference data missing for asset: %s", asset)
		}
		m.assets[id] = ast
	}

	m.assetLiability = make([][]*assetLiability, len(m.memberIndex))

	for i := 0; i < len(m.memberIndex); i++ {
		m.assetLiability[i] = make([]*assetLiability, len(m.assetIndex))
		for j := 0; j < len(m.assetIndex); j++ {
			m.assetLiability[i][j] = &assetLiability{
				Start:  0,
				Trades: make([]*trade, 0),
			}
		}
	}

	for _, trd := range m.trades {
		m.assetLiability[trd.Buyer][trd.QuoteAsset].Trades = append(m.assetLiability[trd.Buyer][trd.QuoteAsset].Trades, trd)
		m.assetLiability[trd.Seller][trd.BaseAsset].Trades = append(m.assetLiability[trd.Seller][trd.BaseAsset].Trades, trd)
	}

	for _, liabilities := range m.assetLiability {
		for _, liability := range liabilities {
			liability.Start = len(liability.Trades) - 1
		}
	}
	slog.Info("Settlement engine initialized.", "trades", len(m.trades), "members", len(m.members), "assets", len(m.assets))
	return nil
}

// calculateNetting applies every trade's full quantity to the netting matrix.
// After this step, a negative netting[member][asset] value means that member's
// current balance is insufficient to cover their net obligation.
func (m *engine) calculateNetting() {
	for _, trd := range m.trades {
		m.netting[trd.Buyer][trd.BaseAsset].Add(m.netting[trd.Buyer][trd.BaseAsset], trd.RemainingBaseQty)
		m.netting[trd.Buyer][trd.QuoteAsset].Sub(m.netting[trd.Buyer][trd.QuoteAsset], trd.RemainingQuoteQty)

		m.netting[trd.Seller][trd.BaseAsset].Sub(m.netting[trd.Seller][trd.BaseAsset], trd.RemainingBaseQty)
		m.netting[trd.Seller][trd.QuoteAsset].Add(m.netting[trd.Seller][trd.QuoteAsset], trd.RemainingQuoteQty)
	}
}

// runIteration finds every member-asset pair with a negative netting balance and
// reverses the minimum amount of the most-recent trades (LIFO order) needed to
// eliminate each deficit. Returns true if any negatives were found so the caller
// knows to run another pass.
func (m *engine) runIteration() bool {
	m.negativeLocations = make([]*location, 0)
	for i, assets := range m.netting {
		for j, value := range assets {
			if value.Sign() < 0 {
				m.negativeLocations = append(m.negativeLocations, newLocation(i, j))
			}
		}
	}

	if len(m.negativeLocations) == 0 {
		return false
	}

	for _, loc := range m.negativeLocations {
		remaining := new(big.Int).Neg(m.netting[loc.member][loc.asset])
		for {
			if remaining.Sign() <= 0 {
				break
			}

			liability := m.assetLiability[loc.member][loc.asset]
			if liability.Start < 0 {
				fmt.Println("Can't be")
			}
			trd := liability.Trades[liability.Start]
			if trd.RemainingBaseQty.Sign() == 0 {
				liability.Start -= 1
				continue
			}

			if trd.BaseAsset == loc.asset {
				if remaining.Cmp(trd.RemainingBaseQty) > 0 {
					qtyToReverse := new(big.Int).Set(trd.RemainingBaseQty)
					m.reverseTrade(trd, qtyToReverse, true)
					remaining.Sub(remaining, qtyToReverse)
					liability.Start -= 1
				} else {
					m.reverseTrade(trd, new(big.Int).Set(remaining), true)
					remaining.SetInt64(0)
				}
			} else if trd.QuoteAsset == loc.asset {
				if remaining.Cmp(trd.RemainingQuoteQty) > 0 {
					qtyToReverse := new(big.Int).Set(trd.RemainingQuoteQty)
					m.reverseTrade(trd, qtyToReverse, false)
					remaining.Sub(remaining, qtyToReverse)
					liability.Start -= 1
				} else {
					m.reverseTrade(trd, new(big.Int).Set(remaining), false)
					remaining.SetInt64(0)
				}
			}
		}
	}
	m.deferFollowingTradesIfNotFullySettledx()
	return true
}

// reverseTrade reduces a trade's remaining quantity by qty and unwinds the
// corresponding netting entries. When base is true, qty is denominated in the
// base asset and the quote amount is derived proportionally; otherwise qty is
// denominated in the quote asset.
func (m *engine) reverseTrade(trade *trade, qty *big.Int, base bool) {
	if trade.BaseQuantity.Sign() == 0 || trade.QuoteQuantity.Sign() == 0 {
		return
	}
	var baseQty, quoteQty *big.Int
	if base {
		baseQty = qty
		quoteQty = multiplyAndDivide(qty, trade.QuoteQuantity, trade.BaseQuantity)
		// Ensure the non-driving side is never zero when the driving side is positive.
		// A zero result from integer truncation would leave RemainingQuoteQty unchanged,
		// causing the line-314 recalculation to restore it to the original value and the
		// trade to be misclassified as FULL.
		if quoteQty.Sign() == 0 && qty.Sign() > 0 {
			quoteQty = big.NewInt(1)
		}
	} else {
		quoteQty = qty
		baseQty = multiplyAndDivide(qty, trade.BaseQuantity, trade.QuoteQuantity)
		// Same guard for the base side: a zero result means RemainingBaseQty is never
		// decremented, so the trade appears to fully settle and stops blocking FIFO.
		if baseQty.Sign() == 0 && qty.Sign() > 0 {
			baseQty = big.NewInt(1)
		}
	}

	// Clamp to avoid over-reversing in the rare case where minimum-1 exceeds what remains.
	if baseQty.Cmp(trade.RemainingBaseQty) > 0 {
		baseQty = new(big.Int).Set(trade.RemainingBaseQty)
	}
	if quoteQty.Cmp(trade.RemainingQuoteQty) > 0 {
		quoteQty = new(big.Int).Set(trade.RemainingQuoteQty)
	}

	trade.RemainingBaseQty.Sub(trade.RemainingBaseQty, baseQty)
	trade.RemainingQuoteQty.Sub(trade.RemainingQuoteQty, quoteQty)

	m.netting[trade.Buyer][trade.BaseAsset].Sub(m.netting[trade.Buyer][trade.BaseAsset], baseQty)
	m.netting[trade.Seller][trade.BaseAsset].Add(m.netting[trade.Seller][trade.BaseAsset], baseQty)
	m.netting[trade.Buyer][trade.QuoteAsset].Add(m.netting[trade.Buyer][trade.QuoteAsset], quoteQty)
	m.netting[trade.Seller][trade.QuoteAsset].Sub(m.netting[trade.Seller][trade.QuoteAsset], quoteQty)
}

// run executes the full settlement pipeline: netting, iterative deficit
// resolution, trade classification, optional strict-FIFO enforcement, instruction
// generation, and batch splitting. It returns the final Results.
func (m *engine) run() Results {
	startTime := time.Now().UnixNano()
	m.calculateNetting()

	i := 1
	for {
		ok := m.runIteration()
		i += 1
		if !ok {
			break
		}
	}

	endTime := time.Now().UnixNano()
	slog.Info("settlement instruction calculation complete.", "iterations", i, "time(ns)", endTime-startTime)

	trades := make([]*TradeResult, 0)

	for member := 0; member < len(m.members); member++ {
		for asset := 0; asset < len(m.assets); asset++ {
			m.netting[member][asset] = new(big.Int).Set(m.ledger[member][asset])
		}
	}

	for j, trd := range m.trades {
		originalTrade := m.originalTrades[j]
		trd.RemainingQuoteQty = multiplyAndDivide(trd.QuoteQuantity, trd.RemainingBaseQty, trd.BaseQuantity)
		status := TradeResultStatusPartial
		if trd.RemainingBaseQty.Sign() == 0 || trd.RemainingQuoteQty.Sign() == 0 {
			trd.RemainingQuoteQty = big.NewInt(0)
			trd.RemainingBaseQty = big.NewInt(0)
			status = TradeResultStatusDeferred
		} else if trd.RemainingBaseQty.Cmp(trd.BaseQuantity) == 0 || trd.RemainingQuoteQty.Cmp(trd.QuoteQuantity) == 0 {
			trd.RemainingQuoteQty = new(big.Int).Set(trd.QuoteQuantity)
			trd.RemainingBaseQty = new(big.Int).Set(trd.BaseQuantity)
			if trd.RemainingBaseQty.Sign() == 0 || trd.RemainingQuoteQty.Sign() == 0 {
				fmt.Println("Can't be")
			}
			status = TradeResultStatusFull
		}

		trdResult := &TradeResult{
			Trade:                 originalTrade,
			Status:                status,
			DeferredQuantity:      toDecimal(new(big.Int).Sub(trd.BaseQuantity, trd.RemainingBaseQty)),
			DeferredQuoteQuantity: toDecimal(new(big.Int).Sub(trd.QuoteQuantity, trd.RemainingQuoteQty)),
			SettledQuantity:       toDecimal(trd.RemainingBaseQty),
			SettledQuoteQuantity:  toDecimal(trd.RemainingQuoteQty),
		}
		trades = append(trades, trdResult)
	}

	m.calculateNetting()

	instructions := make([]*Instruction, 0)

	for member := 0; member < len(m.members); member++ {
		for asset := 0; asset < len(m.assets); asset++ {
			diff := new(big.Int).Sub(m.netting[member][asset], m.ledger[member][asset])
			netAmount := toDecimal(new(big.Int).Abs(diff))
			if netAmount.Sign() == 0 {
				continue
			}

			direction := InstructionDirectionCredit
			if diff.Sign() < 0 {
				direction = InstructionDirectionDebit
			}

			instructions = append(instructions, &Instruction{
				Member:         m.members[member],
				Asset:          m.assets[asset].Symbol(),
				OpeningBalance: toDecimal(m.ledger[member][asset]),
				NetAmount:      toDecimal(new(big.Int).Abs(diff)),
				Direction:      direction,
				ClosingBalance: toDecimal(m.netting[member][asset]),
			})
		}
	}

	fullCount := 0
	partialCount := 0
	deferredCount := 0
	for _, trd := range trades {
		if trd.Status == TradeResultStatusFull {
			fullCount += 1
		} else if trd.Status == TradeResultStatusPartial {
			partialCount += 1
		} else {
			deferredCount += 1
		}
	}

	slog.Info("Settlement instruction generation complete.", "instructions", len(instructions), "fullySettled", fullCount, "partiallySettled", partialCount, "deferred", deferredCount)

	result := &Result{
		Instructions: instructions,
		Trades:       trades,
	}
	batches := splitBatches(result)
	return batches
}

// deferFollowingTradesIfNotFullySettled enforces strict FIFO ordering: any trade
// that shares a member-asset pair with an earlier non-fully-settled trade is
// force-deferred. The netting matrix is rebuilt from the remaining settled trades
// so subsequent passes see the correct balances. Returns true if any trade status
// changed, indicating another pass is required.
func (m *engine) deferFollowingTradesIfNotFullySettled(trades []*TradeResult) {
	for {
		for i := 0; i < len(m.memberIndex); i++ {
			for j := 0; j < len(m.assetIndex); j++ {
				m.netting[i][j] = new(big.Int).Set(m.ledger[i][j])
			}
		}

		modified := false

		deferred := make(map[string]bool)

		for _, trd := range trades {
			buyerKey := fmt.Sprintf("%s-%s", trd.Trade.Buyer(), trd.Trade.QuoteAsset())
			sellerKey := fmt.Sprintf("%s-%s", trd.Trade.Seller(), trd.Trade.BaseAsset())
			if deferred[buyerKey] || deferred[sellerKey] {
				if trd.Status != TradeResultStatusDeferred {
					modified = true
				}
				trd.Status = TradeResultStatusDeferred
				trd.SettledQuoteQuantity = decimal.Zero
				trd.SettledQuantity = decimal.Zero
				trd.DeferredQuantity = trd.Trade.Quantity()
				trd.DeferredQuoteQuantity = trd.Trade.Price().Mul(trd.Trade.Quantity())
				deferred[buyerKey] = true
				deferred[sellerKey] = true
				continue
			}
			if trd.Status != TradeResultStatusFull {
				deferred[buyerKey] = true
				deferred[sellerKey] = true
			}

			if trd.Status == TradeResultStatusDeferred {
				continue
			}
			buyerId := m.memberIndex[trd.Trade.Buyer()]
			sellerId := m.memberIndex[trd.Trade.Seller()]
			baseAssetId := m.assetIndex[trd.Trade.BaseAsset()]
			quoteAssetId := m.assetIndex[trd.Trade.QuoteAsset()]
			m.netting[buyerId][baseAssetId] = new(big.Int).Add(m.netting[buyerId][baseAssetId], fromDecimal(trd.SettledQuantity))
			m.netting[sellerId][quoteAssetId] = new(big.Int).Add(m.netting[sellerId][quoteAssetId], fromDecimal(trd.SettledQuoteQuantity))
			m.netting[buyerId][quoteAssetId] = new(big.Int).Sub(m.netting[buyerId][quoteAssetId], fromDecimal(trd.SettledQuoteQuantity))
			m.netting[sellerId][baseAssetId] = new(big.Int).Sub(m.netting[sellerId][baseAssetId], fromDecimal(trd.SettledQuantity))
		}
		if !modified {
			break
		}
	}

}

func (m *engine) deferFollowingTradesIfNotFullySettledx() {
	for {
		modified := false

		deferred := make(map[string]bool)

		for _, trd := range m.trades {
			buyerKey := fmt.Sprintf("%d-%d", trd.Buyer, trd.QuoteAsset)
			sellerKey := fmt.Sprintf("%d-%d", trd.Seller, trd.BaseAsset)
			revertQty := new(big.Int).Set(trd.RemainingBaseQty)
			if deferred[buyerKey] || deferred[sellerKey] {
				deferred[buyerKey] = true
				deferred[sellerKey] = true
				if revertQty.Sign() == 0 {
					continue
				}
				m.reverseTrade(trd, revertQty, true)
				modified = true
				continue
			}
			if trd.BaseQuantity.Cmp(trd.RemainingBaseQty) == 0 {
				continue
			}
			deferred[buyerKey] = true
			deferred[sellerKey] = true
		}
		if !modified {
			break
		}
	}

}

// printNettingTable writes the current netting matrix to a CSV file at
// data/netting-<index>-x.csv. Used for debugging.
func (m *engine) printNettingTable(index int) error {
	if err := os.MkdirAll("data", 0755); err != nil {
		return err
	}

	f, err := os.Create(fmt.Sprintf("data/netting-%d-x.csv", index))
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	symbols := make([]string, 0, len(m.assets))
	for _, asset := range m.assets {
		symbols = append(symbols, asset.Symbol())
	}
	header := append([]string{"member"}, symbols...)
	if err := w.Write(header); err != nil {
		return err
	}

	for memberId, assets := range m.netting {
		row := make([]string, 1+len(assets))
		row[0] = m.members[memberId]
		for assetId, amount := range assets {
			row[1+assetId] = toDecimal(amount).String()
		}
		if err := w.Write(row); err != nil {
			return err
		}
		w.Flush()
	}

	return w.Error()
}
