package loader

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/shopspring/decimal"
)

// Trade is one row of the trades CSV. Only the fields the engine and the
// output writer need are extracted; account-level identifiers in the CSV are
// ignored.
type Trade struct {
	TradeID    string
	ExecTime   time.Time
	Pair       string
	Base       string
	Quote      string
	Quantity   decimal.Decimal
	Price      decimal.Decimal
	QuoteValue decimal.Decimal
	BuyMember  string
	SellMember string
}

// LoadTrades reads trade rows from the trades CSV. The file may start with a
// UTF-8 BOM (Excel-exported files commonly do); it is stripped if present.
// Timestamps are parsed as RFC3339Nano with a fallback for millisecond-only
// UTC stamps.
func LoadTrades(path string) []*Trade {
	f, err := os.Open(path)
	if err != nil {
		panic(fmt.Sprintf("open trades: %v", err))
	}
	defer f.Close()

	// Strip BOM if present
	buf := make([]byte, 3)
	n, _ := f.Read(buf)
	if n == 3 && buf[0] == 0xEF && buf[1] == 0xBB && buf[2] == 0xBF {
		// BOM consumed, continue
	} else {
		f.Seek(0, io.SeekStart)
	}

	r := csv.NewReader(f)
	r.LazyQuotes = true

	// skip header
	if _, err := r.Read(); err != nil {
		panic(fmt.Sprintf("read trades header: %v", err))
	}

	var list []*Trade
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(fmt.Sprintf("read trades row: %v", err))
		}
		if len(row) < 14 {
			continue
		}

		ts, err := time.Parse(time.RFC3339Nano, row[1])
		if err != nil {
			// try without nanoseconds
			ts, err = time.Parse("2006-01-02T15:04:05.999Z", row[1])
			if err != nil {
				panic(fmt.Sprintf("parse timestamp %q: %v", row[1], err))
			}
		}

		list = append(list, &Trade{
			TradeID:    row[0],
			ExecTime:   ts,
			Pair:       row[2],
			Base:       row[3],
			Quote:      row[4],
			Quantity:   strToDecimal(row[5]),
			Price:      strToDecimal(row[6]),
			QuoteValue: strToDecimal(row[7]),
			BuyMember:  row[8],
			SellMember: row[11],
		})
	}

	return list
}
