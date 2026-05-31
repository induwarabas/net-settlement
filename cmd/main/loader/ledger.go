package loader

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"

	"github.com/shopspring/decimal"
)

type LedgerEntry struct {
	member  string
	asset   string
	balance decimal.Decimal
}

func (l *LedgerEntry) Member() string {
	return l.member
}

func (l *LedgerEntry) Asset() string {
	return l.asset
}

func (l *LedgerEntry) Balance() decimal.Decimal {
	return l.balance
}

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
					member:  row[0],
					asset:   asset,
					balance: strToDecimal(row[i+2]),
				})
			}
		}
	}
	return entries
}
