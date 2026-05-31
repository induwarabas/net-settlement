package main

import (
	"fmt"
	"log/slog"
	"os"
	loader "settlement/cmd/main/loader"
	"settlement/cmd/main/output"
	"settlement/internal/settlement"
)

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
		trds[i] = trade
	}

	leds := make([]settlement.LedgerEntry, len(led))
	for i, ledgerEntry := range led {
		leds[i] = ledgerEntry
	}

	instructions := settlement.GenerateInstructions(trds, leds, true)

	output.WriteInstructions(fmt.Sprintf("%s/settlement-instructions.csv", path), instructions)
	output.WriteTradeDetail(fmt.Sprintf("%s/trade-settlements.csv", path), instructions)

}
