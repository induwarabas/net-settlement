// Command settlement reads trades, ledger balances and asset reference data
// from a dataset folder under data/, runs the settlement engine, and writes
// the resulting settlement instructions and per-trade detail to output/<name>/.
//
// Usage:
//
//	settlement [dataset-name]
//
// If dataset-name is omitted, the available subfolders under data/ are listed
// and the user is prompted to pick one interactively.
//
// Expected input files in data/<dataset-name>/:
//   - trades.csv
//   - ledger.csv
//   - assets.csv
//
// Output files written to output/<dataset-name>/:
//   - settlement-instructions.csv
//   - trade-settlements.csv
package main

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	loader "settlement/cmd/main/loader"
	"settlement/cmd/main/output"
	wrappers "settlement/cmd/main/wappers"
	"settlement/pkg/settlement"
	"sort"
	"strconv"
	"strings"
)

func main() {
	name := selectDataset(os.Args)
	inDir := filepath.Join("data", name)
	outDir := filepath.Join("output", name)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		panic(fmt.Sprintf("create %s: %v", outDir, err))
	}

	slog.Info("Using dataset.", "input", inDir, "output", outDir)

	led := loader.LoadLedger(filepath.Join(inDir, "ledger.csv"))
	trd := loader.LoadTrades(filepath.Join(inDir, "trades.csv"))
	ast := loader.LoadAssets(filepath.Join(inDir, "assets.csv"))

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

	output.WriteInstructions(filepath.Join(outDir, "settlement-instructions.csv"), instructions)
	output.WriteTradeDetail(filepath.Join(outDir, "trade-settlements.csv"), instructions)
}

// selectDataset returns the dataset folder name to use under data/. If args
// has a value at index 1, it is used as-is. Otherwise the subfolders of data/
// are listed and the user is prompted to pick one interactively.
func selectDataset(args []string) string {
	if len(args) >= 2 {
		return args[1]
	}

	const root = "data"
	entries, err := os.ReadDir(root)
	if err != nil {
		panic(fmt.Sprintf("read %s: %v", root, err))
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		panic(fmt.Sprintf("no dataset folders found in %s/", root))
	}

	fmt.Printf("Available datasets in %s/:\n", root)
	for i, n := range names {
		fmt.Printf("  [%d] %s\n", i+1, n)
	}
	fmt.Printf("Select dataset (1-%d): ", len(names))

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		panic(fmt.Sprintf("read selection: %v", err))
	}
	choice := strings.TrimSpace(line)
	n, err := strconv.Atoi(choice)
	if err != nil || n < 1 || n > len(names) {
		panic(fmt.Sprintf("invalid selection: %q", choice))
	}
	return names[n-1]
}
