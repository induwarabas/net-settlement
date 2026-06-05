// Package loader reads settlement input CSV files (trades, ledger balances,
// asset reference data) into plain Go structs. Loaders panic on any I/O or
// parse error since they are only invoked by the CLI entry point.
package loader

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/shopspring/decimal"
)

// Asset is one row of the assets reference CSV. Precision is the number of
// decimal places the asset supports; DustThreshold is the smallest amount the
// engine treats as economically meaningful for that asset.
type Asset struct {
	Symbol        string
	DustThreshold decimal.Decimal
	Precision     int
}

// LoadAssets reads asset reference data from a CSV with header
// "Asset,Dust Threshold,Precision" and returns one Asset per data row.
func LoadAssets(path string) []*Asset {
	f, err := os.Open(path)
	if err != nil {
		panic(fmt.Sprintf("open assets: %v", err))
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.LazyQuotes = true

	// skip header
	if _, err := r.Read(); err != nil {
		panic(fmt.Sprintf("read trades header: %v", err))
	}
	var list []*Asset
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(fmt.Sprintf("read trades row: %v", err))
		}
		if len(row) < 3 {
			continue
		}
		precision, _ := strconv.Atoi(row[2])
		list = append(list, &Asset{
			Symbol:        row[0],
			DustThreshold: strToDecimal(row[1]),
			Precision:     precision,
		})
	}

	return list
}
