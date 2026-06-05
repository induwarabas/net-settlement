// Package wrappers adapts the loader's concrete DTO structs to the read-only
// interfaces (settlement.Trade, settlement.LedgerEntry, settlement.Asset) that
// the settlement engine consumes.
package wrappers

import (
	"settlement/cmd/main/loader"

	"github.com/shopspring/decimal"
)

// TradeWrapper adapts a loader.Trade to the settlement.Trade interface. The
// trade's execution timestamp is pre-converted to nanoseconds at construction
// so the hot-path ExecTime() accessor is allocation-free.
type TradeWrapper struct {
	Trade    *loader.Trade
	execTime int64
}

// NewTradeWrapper returns a TradeWrapper around trade.
func NewTradeWrapper(trade *loader.Trade) *TradeWrapper {
	return &TradeWrapper{
		Trade:    trade,
		execTime: trade.ExecTime.UnixNano(),
	}
}

func (t *TradeWrapper) ExecTime() int64 {
	return t.execTime
}

func (t *TradeWrapper) Buyer() string {
	return t.Trade.BuyMember
}

func (t *TradeWrapper) Seller() string {
	return t.Trade.SellMember
}

func (t *TradeWrapper) BaseAsset() string {
	return t.Trade.Base
}

func (t *TradeWrapper) QuoteAsset() string {
	return t.Trade.Quote
}

func (t *TradeWrapper) Quantity() decimal.Decimal {
	return t.Trade.Quantity
}

func (t *TradeWrapper) Price() decimal.Decimal {
	return t.Trade.Price
}

func (t *TradeWrapper) TradeID() string {
	return t.Trade.TradeID
}

func (t *TradeWrapper) QuoteValue() decimal.Decimal {
	return t.Trade.QuoteValue
}

func (t *TradeWrapper) Pair() string {
	return t.Trade.Pair
}

// LedgerEntryWrapper adapts a loader.LedgerEntry to the settlement.LedgerEntry
// interface.
type LedgerEntryWrapper struct {
	ledgerEntry *loader.LedgerEntry
}

// NewLedgerEntryWrapper returns a LedgerEntryWrapper around ledgerEntry.
func NewLedgerEntryWrapper(ledgerEntry *loader.LedgerEntry) *LedgerEntryWrapper {
	return &LedgerEntryWrapper{
		ledgerEntry: ledgerEntry,
	}
}

func (l *LedgerEntryWrapper) Member() string {
	return l.ledgerEntry.Member
}

func (l *LedgerEntryWrapper) Asset() string {
	return l.ledgerEntry.Asset
}

func (l *LedgerEntryWrapper) Balance() decimal.Decimal {
	return l.ledgerEntry.Balance
}

// AssetWrapper adapts a loader.Asset to the settlement.Asset interface.
type AssetWrapper struct {
	asset *loader.Asset
}

// NewAssetWrapper returns an AssetWrapper around asset.
func NewAssetWrapper(asset *loader.Asset) *AssetWrapper {
	return &AssetWrapper{
		asset: asset,
	}
}

func (a *AssetWrapper) Symbol() string {
	return a.asset.Symbol
}

func (a *AssetWrapper) Precision() int {
	return a.asset.Precision
}

func (a *AssetWrapper) DustThreshold() decimal.Decimal {
	return a.asset.DustThreshold
}
