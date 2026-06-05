package main

import (
	"fmt"
	"log/slog"
	"os"
	loader "settlement/cmd/main/loader"
	"settlement/cmd/main/output"
	"settlement/pkg/settlement"

	"github.com/shopspring/decimal"
)

type tradeWrapper struct {
	Trade    *loader.Trade
	execTime int64
}

func newTradeWrapper(trade *loader.Trade) *tradeWrapper {
	return &tradeWrapper{
		Trade:    trade,
		execTime: trade.ExecTime.UnixNano(),
	}
}

func (t *tradeWrapper) ExecTime() int64 {
	return t.execTime
}

func (t *tradeWrapper) Buyer() string {
	return t.Trade.BuyMember
}

func (t *tradeWrapper) Seller() string {
	return t.Trade.SellMember
}

func (t *tradeWrapper) BaseAsset() string {
	return t.Trade.Base
}

func (t *tradeWrapper) QuoteAsset() string {
	return t.Trade.Quote
}

func (t *tradeWrapper) Quantity() decimal.Decimal {
	return t.Trade.Quantity
}

func (t *tradeWrapper) Price() decimal.Decimal {
	return t.Trade.Price
}

func (t *tradeWrapper) TradeID() string {
	return t.Trade.TradeID
}

func (t *tradeWrapper) QuoteValue() decimal.Decimal {
	return t.Trade.QuoteValue
}

func (t *tradeWrapper) Pair() string {
	return t.Trade.Pair
}

type ledgerEntryWrapper struct {
	ledgerEntry *loader.LedgerEntry
}

func newLedgerEntryWrapper(ledgerEntry *loader.LedgerEntry) *ledgerEntryWrapper {
	return &ledgerEntryWrapper{
		ledgerEntry: ledgerEntry,
	}
}

func (l *ledgerEntryWrapper) Member() string {
	return l.ledgerEntry.Member
}

func (l *ledgerEntryWrapper) Asset() string {
	return l.ledgerEntry.Asset
}

func (l *ledgerEntryWrapper) Balance() decimal.Decimal {
	return l.ledgerEntry.Balance
}

func main() {
	path := "data"
	if len(os.Args) >= 2 {
		path = os.Args[1]
	}

	slog.Info("Using data folder.", "path", path)

	led := loader.LoadLedger(fmt.Sprintf("%s/ledger.csv", path))
	trd := loader.LoadTrades(fmt.Sprintf("%s/trades.csv", path))

	trds := make([]settlement.Trade, len(trd))
	for i, trade := range trd {
		trds[i] = newTradeWrapper(trade)
	}

	leds := make([]settlement.LedgerEntry, len(led))
	for i, ledgerEntry := range led {
		leds[i] = newLedgerEntryWrapper(ledgerEntry)
	}

	instructions := settlement.GenerateInstructions(trds, leds, true)

	output.WriteInstructions(fmt.Sprintf("%s/settlement-instructions.csv", path), instructions)
	output.WriteTradeDetail(fmt.Sprintf("%s/trade-settlements.csv", path), instructions)

}
