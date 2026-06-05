package loader

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/shopspring/decimal"
)

type Asset struct {
	Symbol        string
	DustThreshold decimal.Decimal
	Precision     int
}

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
