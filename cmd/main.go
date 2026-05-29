package main

import (
	"fmt"
	"log/slog"
	"os"
	loader2 "settlement/cmd/loader"
	"settlement/cmd/output"
	"settlement/internal/settlement"
)

func main() {
	path := "data"
	if len(os.Args) >= 2 {
		path = os.Args[1]
	}

	slog.Info("Using data folder.", "path", path)

	led := loader2.LoadLedger(fmt.Sprintf("%s/ledger.csv", path))
	trd := loader2.LoadTrades(fmt.Sprintf("%s/trades.csv", path))

	trds := make([]settlement.Trade, len(trd))
	for i, trade := range trd {
		trds[i] = trade
	}

	leds := make([]settlement.LedgerEntry, len(led))
	for i, ledgerEntry := range led {
		leds[i] = ledgerEntry
	}

	instructions := settlement.GenerateInstructions(trds, leds)

	output.WriteInstructions(fmt.Sprintf("%s/settlement-instructions.csv", path), instructions.Instructions)
	output.WriteTradeDetail(fmt.Sprintf("%s/trade-settlements.csv", path), instructions.Trades)
}
