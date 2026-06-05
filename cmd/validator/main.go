package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

type tradeInfo struct {
	buyer  string
	seller string
}

type settlementRow struct {
	tradeID         string
	execTime        time.Time
	base            string
	quote           string
	settledQty      decimal.Decimal
	settledQuoteVal decimal.Decimal
	status          string
}

type instructionRow struct {
	member    string
	asset     string
	netAmount decimal.Decimal
	direction string
}

type memberAsset struct {
	member string
	asset  string
}

func main() {
	path := "data"
	if len(os.Args) >= 2 {
		path = os.Args[1]
	}

	slog.Info("Validating settlement output.", "path", path)

	trades := loadTradesCSV(path + "/trades.csv")
	settlements := loadSettlementsCSV(path + "/trade-settlements.csv")
	instructions := loadInstructionsCSV(path + "/settlement-instructions.csv")

	errors := 0
	errors += validateNetAmounts(trades, settlements, instructions)
	errors += validateFIFO(trades, settlements)

	if errors == 0 {
		slog.Info("All validations passed.")
	} else {
		slog.Error("Validation failed.", "errors", errors)
		os.Exit(1)
	}
}

// precision is the tolerance for net amount comparisons; it covers rounding
// introduced by the engine's integer division in multiplyAndDivide.
var precision = decimal.New(1, -4) // 0.0001

// validateNetAmounts checks that the net settled trade amounts per member+asset
// match the directions and amounts in the settlement instructions.
func validateNetAmounts(trades map[string]*tradeInfo, settlements []*settlementRow, instructions []*instructionRow) int {
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
		absNet := netVal.Abs()

		// Skip tiny amounts that are below the engine's rounding precision.
		if absNet.LessThan(precision) {
			if inst, exists := instrMap[k]; exists && inst.netAmount.GreaterThanOrEqual(precision) {
				slog.Error("Instruction exists for negligible net trade movement.", "member", k.member, "asset", k.asset, "net", netVal, "instruction", inst.netAmount)
				errors++
			}
			continue
		}

		inst, exists := instrMap[k]
		if !exists {
			slog.Error("Missing settlement instruction for net trade movement.", "member", k.member, "asset", k.asset, "net", netVal)
			errors++
			continue
		}

		wantDir := "IN"
		wantAmt := netVal
		if netVal.IsNegative() {
			wantDir = "OUT"
			wantAmt = netVal.Neg()
		}

		if inst.direction != wantDir {
			slog.Error("Instruction direction mismatch.", "member", k.member, "asset", k.asset, "want", wantDir, "got", inst.direction)
			errors++
		}
		diff := wantAmt.Sub(inst.netAmount).Abs()
		if diff.GreaterThanOrEqual(precision) {
			slog.Error("Instruction net amount mismatch.", "member", k.member, "asset", k.asset, "want", wantAmt, "got", inst.netAmount, "diff", diff)
			errors++
		}

		delete(instrMap, k)
	}

	for k, inst := range instrMap {
		if inst.netAmount.GreaterThanOrEqual(precision) {
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
			tradeID:         strings.TrimSpace(row[1]),
			execTime:        ts,
			base:            strings.TrimSpace(row[4]),
			quote:           strings.TrimSpace(row[5]),
			settledQty:      parseDecimal(row[10]),
			settledQuoteVal: parseDecimal(row[11]),
			status:          strings.TrimSpace(row[14]),
		})
	}
	return rows
}

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

func parseDecimal(s string) decimal.Decimal {
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimSpace(s)
	if s == "" {
		return decimal.Zero
	}
	return decimal.RequireFromString(s)
}
