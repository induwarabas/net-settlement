package output

import (
	"encoding/csv"
	"fmt"
	"os"
	"settlement/cmd/main/loader"
	"settlement/pkg/settlement"
	"sort"
	"time"
)

func WriteInstructions(path string, results settlement.Results) {
	type entry struct {
		batch int
		inst  *settlement.Instruction
	}
	var entries []entry
	for i, batch := range results.Batches {
		for _, inst := range batch.Instructions {
			entries = append(entries, entry{i + 1, inst})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.batch != b.batch {
			return a.batch < b.batch
		}
		if a.inst.Member != b.inst.Member {
			return a.inst.Member < b.inst.Member
		}
		return a.inst.Asset < b.inst.Asset
	})

	f, err := os.Create(path)
	if err != nil {
		panic(fmt.Sprintf("create %s: %v", path, err))
	}
	defer f.Close()

	w := csv.NewWriter(f)
	w.Write([]string{"Batch", "Member", "Tier", "Asset", "OpeningBalance", "NetAmount", "Direction", "ClosingBalance"})

	for _, e := range entries {
		inst := e.inst
		w.Write([]string{
			fmt.Sprintf("%d", e.batch),
			inst.Member,
			"",
			inst.Asset,
			inst.OpeningBalance.String(),
			inst.NetAmount.String(),
			string(inst.Direction),
			inst.ClosingBalance.String(),
		})
	}
	w.Flush()
}

func WriteTradeDetail(path string, results settlement.Results) {
	f, err := os.Create(path)
	if err != nil {
		panic(fmt.Sprintf("create %s: %v", path, err))
	}
	defer f.Close()

	w := csv.NewWriter(f)
	w.Write([]string{
		"Batch", "TradeId", "ExecutionTimestamp", "Pair", "Base", "Quote",
		"OriginalQuantity", "OriginalQuoteValue",
		"Buyer", "Seller",
		"SettledQuantity", "SettledQuoteValue",
		"DeferredQuantity", "DeferredQuoteValue",
		"Status",
	})

	for i, batch := range results.Batches {
		batchLabel := fmt.Sprintf("%d", i+1)
		for _, r := range batch.Trades {
			t := r.Trade.(*loader.Trade)
			w.Write([]string{
				batchLabel,
				t.TradeID(),
				time.Unix(0, t.ExecTime()).UTC().Format("2006-01-02T15:04:05.999Z07:00"),
				t.Pair(),
				t.BaseAsset(),
				t.QuoteAsset(),
				t.Quantity().String(),
				t.QuoteValue().String(),
				t.Buyer(),
				t.Seller(),
				r.SettledQuantity.String(),
				r.SettledQuoteQuantity.String(),
				r.DeferredQuantity.String(),
				r.DeferredQuoteQuantity.String(),
				string(r.Status),
			})
		}
	}

	for _, trade := range results.Deferred {
		t := trade.(*loader.Trade)
		qty := t.Quantity()
		quoteVal := t.QuoteValue()
		w.Write([]string{
			"",
			t.TradeID(),
			time.Unix(0, t.ExecTime()).UTC().Format("2006-01-02T15:04:05.999Z07:00"),
			t.Pair(),
			t.BaseAsset(),
			t.QuoteAsset(),
			qty.String(),
			quoteVal.String(),
			t.Buyer(),
			t.Seller(),
			"0",
			"0",
			qty.String(),
			quoteVal.String(),
			string(settlement.TradeResultStatusDeferred),
		})
	}
	w.Flush()
}
