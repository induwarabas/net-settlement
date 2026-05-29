package loader

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/shopspring/decimal"
)

type Trade struct {
	tradeID    string
	execTime   time.Time
	pair       string
	base       string
	quote      string
	quantity   decimal.Decimal
	price      decimal.Decimal
	quoteValue decimal.Decimal
	buyMember  string
	sellMember string
}

func (t *Trade) ExecTime() int64 {
	return int64(t.execTime.UnixNano())
}

func (t *Trade) Buyer() string {
	return t.buyMember
}

func (t *Trade) Seller() string {
	return t.sellMember
}

func (t *Trade) BaseAsset() string {
	return t.base
}

func (t *Trade) QuoteAsset() string {
	return t.quote
}

func (t *Trade) Quantity() decimal.Decimal {
	return t.quantity
}

func (t *Trade) Price() decimal.Decimal {
	return t.price
}

func (t *Trade) TradeID() string {
	return t.tradeID
}

func (t *Trade) QuoteValue() decimal.Decimal {
	return t.quoteValue
}

func (t *Trade) Pair() string {
	return t.pair
}

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
			tradeID:    row[0],
			execTime:   ts,
			pair:       row[2],
			base:       row[3],
			quote:      row[4],
			quantity:   strToDecimal(row[5]),
			price:      strToDecimal(row[6]),
			quoteValue: strToDecimal(row[7]),
			buyMember:  row[8],
			sellMember: row[11],
		})
	}

	return list
}
