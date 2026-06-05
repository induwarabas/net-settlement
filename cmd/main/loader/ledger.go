package loader

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"

	"github.com/shopspring/decimal"
)

// LedgerEntry is a single (member, asset, balance) triple expanded from the
// wide-format ledger CSV.
type LedgerEntry struct {
	Member  string
	Asset   string
	Balance decimal.Decimal
}

// LoadLedger reads the wide-format ledger CSV (one row per member, one column
// per asset after the Member and Tier columns) and flattens it into a slice of
// LedgerEntry. Empty cells are skipped — they represent "no balance" rather
// than "zero balance".
func LoadLedger(path string) []*LedgerEntry {
	f, err := os.Open(path)
	if err != nil {
		panic(fmt.Sprintf("open ledger: %v", err))
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.LazyQuotes = true

	header, err := r.Read()
	if err != nil {
		panic(fmt.Sprintf("read ledger header: %v", err))
	}
	// header: Member, Tier, <asset columns...>
	assets := header[2:]

	entries := make([]*LedgerEntry, 0)
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(fmt.Sprintf("read ledger row: %v", err))
		}
		for i, asset := range assets {
			if i+2 < len(row) && row[i+2] != "" {
				entries = append(entries, &LedgerEntry{
					Member:  row[0],
					Asset:   asset,
					Balance: strToDecimal(row[i+2]),
				})
			}
		}
	}
	return entries
}
