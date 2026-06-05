// Command validator cross-checks the output of the settlement engine against
// its input trades. It loads trades.csv from data/<dataset-name>/ and the
// engine's outputs (trade-settlements.csv, settlement-instructions.csv) from
// output/<dataset-name>/ and verifies:
//
//  1. Net per-(member, asset) trade movements reconcile with the issued
//     settlement instructions (within engine rounding precision).
//  2. Strict FIFO ordering — once a trade for a given (member, asset) debit
//     position is PARTIAL or DEFERRED, all later trades for that position
//     must be DEFERRED.
//
// Exits with status 1 if any check fails.
//
// Usage:
//
//	validator [dataset-name]
//
// If dataset-name is omitted, the available subfolders under data/ are listed
// and the user is prompted to pick one interactively.
package main

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// tradeInfo carries just the buyer/seller pair the validator needs from each
// row of trades.csv.
type tradeInfo struct {
	buyer  string
	seller string
}

// settlementRow is a parsed row of trade-settlements.csv.
type settlementRow struct {
	tradeID          string
	execTime         time.Time
	base             string
	quote            string
	settledQty       decimal.Decimal
	settledQuoteVal  decimal.Decimal
	deferredQty      decimal.Decimal
	deferredQuoteVal decimal.Decimal
	status           string
}

// assetInfo carries the reference-data fields the validator needs about an
// asset.
type assetInfo struct {
	precision int
	dust      decimal.Decimal
}

// instructionRow is a parsed row of settlement-instructions.csv.
type instructionRow struct {
	member    string
	asset     string
	netAmount decimal.Decimal
	direction string
}

// memberAsset is the composite key used to index per-position net movements
// and settlement instructions.
type memberAsset struct {
	member string
	asset  string
}

func main() {
	name := selectDataset(os.Args)
	inDir := filepath.Join("data", name)
	outDir := filepath.Join("output", name)

	slog.Info("Validating settlement output.", "input", inDir, "output", outDir)

	trades := loadTradesCSV(filepath.Join(inDir, "trades.csv"))
	assets := loadAssetsCSV(filepath.Join(inDir, "assets.csv"))
	settlements := loadSettlementsCSV(filepath.Join(outDir, "trade-settlements.csv"))
	instructions := loadInstructionsCSV(filepath.Join(outDir, "settlement-instructions.csv"))

	checks := []struct {
		name string
		run  func() int
	}{
		{"Net amounts", func() int {
			return validateNetAmounts(trades, settlements, instructions, assets)
		}},
		{"FIFO ordering", func() int {
			return validateFIFO(trades, settlements)
		}},
		{"Dust thresholds", func() int {
			return validateDust(settlements, assets)
		}},
	}

	total := 0
	for i, c := range checks {
		slog.Info("Running check.", "step", fmt.Sprintf("%d/%d", i+1, len(checks)), "check", c.name)
		n := c.run()
		if n == 0 {
			slog.Info("Check passed.", "check", c.name)
		} else {
			slog.Error("Check failed.", "check", c.name, "errors", n)
		}
		total += n
	}

	if total == 0 {
		slog.Info("All validations passed.", "checks", len(checks))
	} else {
		slog.Error("Validation failed.", "errors", total)
		os.Exit(1)
	}
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

// validateNetAmounts checks that the net settled trade amounts per member+asset
// match the directions and amounts in the settlement instructions. The engine
// rounds each instruction's NetAmount down to the asset's declared precision,
// so the computed net is rounded the same way before comparison; a tolerance of
// one precision step covers any sub-precision rounding drift between the two
// independent accumulators.
func validateNetAmounts(trades map[string]*tradeInfo, settlements []*settlementRow, instructions []*instructionRow, assets map[string]assetInfo) int {
	net := make(map[memberAsset]decimal.Decimal)

	for _, s := range settlements {
		t, ok := trades[s.tradeID]
		if !ok {
			slog.Warn("Trade not found in trades.csv; skipping.", "tradeID", s.tradeID)
			continue
		}
		if s.settledQty.IsZero() {
			continue
		}
		buyerBase := memberAsset{t.buyer, s.base}
		buyerQuote := memberAsset{t.buyer, s.quote}
		sellerBase := memberAsset{t.seller, s.base}
		sellerQuote := memberAsset{t.seller, s.quote}

		net[buyerBase] = net[buyerBase].Add(s.settledQty)
		net[buyerQuote] = net[buyerQuote].Sub(s.settledQuoteVal)
		net[sellerBase] = net[sellerBase].Sub(s.settledQty)
		net[sellerQuote] = net[sellerQuote].Add(s.settledQuoteVal)
	}

	instrMap := make(map[memberAsset]*instructionRow)
	for _, inst := range instructions {
		instrMap[memberAsset{inst.member, inst.asset}] = inst
	}

	errors := 0

	for k, netVal := range net {
		ai, ok := assets[k.asset]
		if !ok {
			slog.Error("Asset reference data not found in assets.csv.", "asset", k.asset)
			errors++
			continue
		}
		p := ai.precision

		// Round the computed net down to the asset's precision and use one
		// precision step as the comparison tolerance — matching how the engine
		// emits instruction NetAmounts.
		wantDir := "IN"
		absNet := netVal
		if netVal.IsNegative() {
			wantDir = "OUT"
			absNet = netVal.Neg()
		}
		wantAmt := absNet.Truncate(int32(p))
		tol := decimal.New(1, int32(-p)) // 10^(-p)

		if wantAmt.IsZero() {
			if inst, exists := instrMap[k]; exists && inst.netAmount.GreaterThanOrEqual(tol) {
				slog.Error("Instruction exists for negligible net trade movement.", "member", k.member, "asset", k.asset, "net", netVal, "instruction", inst.netAmount)
				errors++
			}
			delete(instrMap, k)
			continue
		}

		inst, exists := instrMap[k]
		if !exists {
			slog.Error("Missing settlement instruction for net trade movement.", "member", k.member, "asset", k.asset, "net", netVal)
			errors++
			continue
		}

		if inst.direction != wantDir {
			slog.Error("Instruction direction mismatch.", "member", k.member, "asset", k.asset, "want", wantDir, "got", inst.direction)
			errors++
		}
		diff := wantAmt.Sub(inst.netAmount).Abs()
		if diff.GreaterThan(tol) {
			slog.Error("Instruction net amount mismatch.", "member", k.member, "asset", k.asset, "want", wantAmt, "got", inst.netAmount, "diff", diff, "precision", p)
			errors++
		}

		delete(instrMap, k)
	}

	for k, inst := range instrMap {
		ai, ok := assets[k.asset]
		if !ok {
			slog.Error("Asset reference data not found in assets.csv.", "asset", k.asset)
			errors++
			continue
		}
		tol := decimal.New(1, int32(-ai.precision))
		if inst.netAmount.GreaterThanOrEqual(tol) {
			slog.Error("Instruction with no corresponding trade activity.", "member", k.member, "asset", k.asset, "amount", inst.netAmount, "direction", inst.direction)
			errors++
		}
	}

	return errors
}

// validateFIFO checks that for each (member, asset) debit position, trades are
// settled in execution-time order: once a PARTIAL or DEFERRED trade appears, all
// subsequent trades for that position must be DEFERRED.
func validateFIFO(trades map[string]*tradeInfo, settlements []*settlementRow) int {
	type debitEntry struct {
		tradeID  string
		execTime time.Time
		status   string
	}

	debitMap := make(map[memberAsset][]debitEntry)

	for _, s := range settlements {
		t, ok := trades[s.tradeID]
		if !ok {
			continue
		}
		entry := debitEntry{s.tradeID, s.execTime, s.status}
		// Buyer debits quote asset; seller debits base asset.
		debitMap[memberAsset{t.buyer, s.quote}] = append(debitMap[memberAsset{t.buyer, s.quote}], entry)
		debitMap[memberAsset{t.seller, s.base}] = append(debitMap[memberAsset{t.seller, s.base}], entry)
	}

	errors := 0
	for key, entries := range debitMap {
		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].execTime.Equal(entries[j].execTime) {
				ni, erri := strconv.Atoi(entries[i].tradeID)
				nj, errj := strconv.Atoi(entries[j].tradeID)
				if erri == nil && errj == nil {
					return ni < nj
				}
				return entries[i].tradeID < entries[j].tradeID
			}
			return entries[i].execTime.Before(entries[j].execTime)
		})

		// Once we see a PARTIAL or DEFERRED, all subsequent must be DEFERRED.
		firstNonFullID := ""
		for _, e := range entries {
			if firstNonFullID != "" {
				if e.status != "DEFERRED" {
					slog.Error("FIFO violation: trade settled after earlier partial/deferred trade.",
						"member", key.member, "asset", key.asset,
						"earlierTradeID", firstNonFullID,
						"laterTradeID", e.tradeID, "status", e.status,
					)
					errors++
				}
			} else if e.status == "PARTIAL" || e.status == "DEFERRED" {
				firstNonFullID = e.tradeID
			}
		}
	}
	return errors
}

// validateDust checks that every PARTIAL trade has settled and deferred amounts
// at or above the dust threshold on both base and quote sides. A sub-dust
// settled portion means the trade should have been fully deferred; a sub-dust
// deferred portion means it should have settled in full. Both indicate the
// engine's dust contract was violated.
func validateDust(settlements []*settlementRow, assets map[string]assetInfo) int {
	errors := 0
	for _, s := range settlements {
		if s.status != "PARTIAL" {
			continue
		}
		base, ok := assets[s.base]
		if !ok {
			slog.Error("Asset reference data not found in assets.csv.", "asset", s.base, "tradeID", s.tradeID)
			errors++
			continue
		}
		quote, ok := assets[s.quote]
		if !ok {
			slog.Error("Asset reference data not found in assets.csv.", "asset", s.quote, "tradeID", s.tradeID)
			errors++
			continue
		}

		amounts := []struct {
			side  string
			asset string
			val   decimal.Decimal
			dust  decimal.Decimal
		}{
			{"settled base", s.base, s.settledQty, base.dust},
			{"settled quote", s.quote, s.settledQuoteVal, quote.dust},
			{"deferred base", s.base, s.deferredQty, base.dust},
			{"deferred quote", s.quote, s.deferredQuoteVal, quote.dust},
		}
		for _, a := range amounts {
			if a.val.LessThan(a.dust) {
				slog.Error("Partial trade sub-dust amount.",
					"tradeID", s.tradeID, "side", a.side, "asset", a.asset,
					"amount", a.val, "dust", a.dust,
				)
				errors++
			}
		}
	}
	return errors
}

// loadAssetsCSV reads assets.csv (handling an optional UTF-8 BOM) into an
// asset-symbol -> assetInfo map (precision + dust threshold).
func loadAssetsCSV(path string) map[string]assetInfo {
	f, err := os.Open(path)
	if err != nil {
		panic(fmt.Sprintf("open %s: %v", path, err))
	}
	defer f.Close()

	buf := make([]byte, 3)
	n, _ := f.Read(buf)
	if !(n == 3 && buf[0] == 0xEF && buf[1] == 0xBB && buf[2] == 0xBF) {
		f.Seek(0, io.SeekStart)
	}

	r := csv.NewReader(f)
	r.LazyQuotes = true
	r.Read() // skip header

	out := make(map[string]assetInfo)
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(fmt.Sprintf("read %s: %v", path, err))
		}
		if len(row) < 3 {
			continue
		}
		symbol := strings.TrimSpace(row[0])
		dust := parseDecimal(row[1])
		p, err := strconv.Atoi(strings.TrimSpace(row[2]))
		if err != nil {
			panic(fmt.Sprintf("parse precision for %s: %v", symbol, err))
		}
		out[symbol] = assetInfo{precision: p, dust: dust}
	}
	return out
}

// loadTradesCSV reads trades.csv (handling an optional UTF-8 BOM) into a
// tradeID -> tradeInfo map, keeping only the fields the validator needs.
func loadTradesCSV(path string) map[string]*tradeInfo {
	f, err := os.Open(path)
	if err != nil {
		panic(fmt.Sprintf("open %s: %v", path, err))
	}
	defer f.Close()

	// Strip BOM if present.
	buf := make([]byte, 3)
	n, _ := f.Read(buf)
	if !(n == 3 && buf[0] == 0xEF && buf[1] == 0xBB && buf[2] == 0xBF) {
		f.Seek(0, io.SeekStart)
	}

	r := csv.NewReader(f)
	r.LazyQuotes = true
	r.Read() // skip header

	result := make(map[string]*tradeInfo)
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(fmt.Sprintf("read %s: %v", path, err))
		}
		if len(row) < 12 {
			continue
		}
		id := strings.TrimSpace(row[0])
		result[id] = &tradeInfo{
			buyer:  strings.TrimSpace(row[8]),
			seller: strings.TrimSpace(row[11]),
		}
	}
	return result
}

// loadSettlementsCSV reads trade-settlements.csv into a slice of
// settlementRow, preserving file order.
func loadSettlementsCSV(path string) []*settlementRow {
	f, err := os.Open(path)
	if err != nil {
		panic(fmt.Sprintf("open %s: %v", path, err))
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.Read() // skip header

	var rows []*settlementRow
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(fmt.Sprintf("read %s: %v", path, err))
		}
		if len(row) < 15 {
			continue
		}

		ts, err := parseTimestamp(row[2])
		if err != nil {
			panic(fmt.Sprintf("parse timestamp %q: %v", row[2], err))
		}

		rows = append(rows, &settlementRow{
			tradeID:          strings.TrimSpace(row[1]),
			execTime:         ts,
			base:             strings.TrimSpace(row[4]),
			quote:            strings.TrimSpace(row[5]),
			settledQty:       parseDecimal(row[10]),
			settledQuoteVal:  parseDecimal(row[11]),
			deferredQty:      parseDecimal(row[12]),
			deferredQuoteVal: parseDecimal(row[13]),
			status:           strings.TrimSpace(row[14]),
		})
	}
	return rows
}

// loadInstructionsCSV reads settlement-instructions.csv into a slice of
// instructionRow.
func loadInstructionsCSV(path string) []*instructionRow {
	f, err := os.Open(path)
	if err != nil {
		panic(fmt.Sprintf("open %s: %v", path, err))
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.Read() // skip header

	var rows []*instructionRow
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(fmt.Sprintf("read %s: %v", path, err))
		}
		if len(row) < 7 {
			continue
		}
		rows = append(rows, &instructionRow{
			member:    strings.TrimSpace(row[1]),
			asset:     strings.TrimSpace(row[3]),
			netAmount: parseDecimal(row[5]),
			direction: strings.TrimSpace(row[6]),
		})
	}
	return rows
}

// parseTimestamp parses a settlement-output timestamp, accepting RFC3339Nano
// and a few common millisecond / second variants the engine may emit.
func parseTimestamp(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	formats := []string{
		time.RFC3339Nano,
		"2006-01-02T15:04:05.999Z07:00",
		"2006-01-02T15:04:05.999Z",
		"2006-01-02T15:04:05Z",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised timestamp format: %q", s)
}

// parseDecimal parses a CSV numeric cell into a decimal.Decimal, stripping
// thousands separators and surrounding whitespace. An empty cell parses as
// zero; any other unparseable input panics.
func parseDecimal(s string) decimal.Decimal {
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimSpace(s)
	if s == "" {
		return decimal.Zero
	}
	return decimal.RequireFromString(s)
}
