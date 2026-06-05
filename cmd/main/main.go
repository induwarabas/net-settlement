// Command settlement reads trades, ledger balances and asset reference data
// from a CSV folder, runs the settlement engine, and writes the resulting
// settlement instructions and per-trade detail back into the same folder.
//
// Usage:
//
//	settlement [data-folder]
//
// The data folder defaults to "data". Expected input files:
//   - trades.csv
//   - ledger.csv
//   - assets.csv
//
// Output files written next to the inputs:
//   - settlement-instructions.csv
//   - trade-settlements.csv
package main

import (
	"fmt"
	"log/slog"
	"os"
	loader "settlement/cmd/main/loader"
	"settlement/cmd/main/output"
	wrappers "settlement/cmd/main/wappers"
	"settlement/pkg/settlement"
)

func main() {
	path := "data"
	if len(os.Args) >= 2 {
		path = os.Args[1]
	}

	slog.Info("Using data folder.", "path", path)

	led := loader.LoadLedger(fmt.Sprintf("%s/ledger.csv", path))
	trd := loader.LoadTrades(fmt.Sprintf("%s/trades.csv", path))
	ast := loader.LoadAssets(fmt.Sprintf("%s/assets.csv", path))

	trds := make([]settlement.Trade, len(trd))
	for i, trade := range trd {
		trds[i] = wrappers.NewTradeWrapper(trade)
	}

	leds := make([]settlement.LedgerEntry, len(led))
	for i, ledgerEntry := range led {
		leds[i] = wrappers.NewLedgerEntryWrapper(ledgerEntry)
	}

	asts := make([]settlement.Asset, len(ast))
	for i, asset := range ast {
		asts[i] = wrappers.NewAssetWrapper(asset)
	}

	instructions := settlement.GenerateInstructions(trds, leds, asts, true)

	output.WriteInstructions(fmt.Sprintf("%s/settlement-instructions.csv", path), instructions)
	output.WriteTradeDetail(fmt.Sprintf("%s/trade-settlements.csv", path), instructions)

}
