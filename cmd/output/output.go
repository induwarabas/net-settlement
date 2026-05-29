package output

import (
	"encoding/csv"
	"fmt"
	"os"
	"settlement/cmd/loader"
	"settlement/internal/settlement"
	"sort"
	"time"
)

func WriteInstructions(path string, instructions []*settlement.Instruction) {
	// Sort for deterministic output: Member, Asset
	sort.Slice(instructions, func(i, j int) bool {
		a, b := instructions[i], instructions[j]
		if a.Member != b.Member {
			return a.Member < b.Member
		}
		return a.Asset < b.Asset
	})

	f, err := os.Create(path)
	if err != nil {
		panic(fmt.Sprintf("create %s: %v", path, err))
	}
	defer f.Close()

	w := csv.NewWriter(f)
	w.Write([]string{"Member", "Tier", "Asset", "OpeningBalance", "NetAmount", "Direction", "ClosingBalance"})

	for _, inst := range instructions {
		w.Write([]string{
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

func WriteTradeDetail(path string, results []*settlement.TradeResult) {
	f, err := os.Create(path)
	if err != nil {
		panic(fmt.Sprintf("create %s: %v", path, err))
	}
	defer f.Close()

	w := csv.NewWriter(f)
	w.Write([]string{
		"TradeId", "ExecutionTimestamp", "Pair", "Base", "Quote",
		"OriginalQuantity", "OriginalQuoteValue",
		"SettledQuantity", "SettledQuoteValue",
		"DeferredQuantity", "DeferredQuoteValue",
		"Status",
	})

	for _, r := range results {
		t := r.Trade.(*loader.Trade)
		w.Write([]string{
			t.TradeID(),
			time.Unix(0, t.ExecTime()).UTC().Format("2006-01-02T15:04:05.999Z07:00"),
			t.Pair(),
			t.BaseAsset(),
			t.QuoteAsset(),
			t.Quantity().String(),
			t.QuoteValue().String(),
			r.SettledQuantity.String(),
			r.SettledQuoteQuantity.String(),
			r.DeferredQuantity.String(),
			r.DeferredQuoteQuantity.String(),
			string(r.Status),
		})
	}
	w.Flush()
}
