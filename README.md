# settlement

Multilateral trade settlement engine written in Go. Given a set of trades, member ledger balances, and asset metadata, it produces settlement instructions (debits/credits) and classifies each trade as fully settled, partially settled, or deferred.

For the algorithm details, see [Pseudo.md](./Pseudo.md).

## Requirements

- Go 1.26 or newer
- `git`

The only third-party dependency is `github.com/shopspring/decimal`, fetched automatically by `go mod`.

## Setup

```bash
git clone <repo-url> settlement
cd settlement
go mod download
```

## Project layout

```
cmd/
  main/         # settlement engine CLI — reads CSV input, writes CSV output
  validator/    # validates engine output against the input trades
pkg/
  settlement/   # core engine (engine.go, batch.go, numeric.go, settlement.go)
data/           # sample input + output CSV set (default path)
data1..data4/   # additional sample datasets
Pseudo.md       # algorithm reference
```

## Input files

Each data folder must contain:

| File          | Purpose                                                    |
|---------------|------------------------------------------------------------|
| `trades.csv`  | Executed trades to settle                                  |
| `ledger.csv`  | Opening member balances per asset                          |
| `assets.csv`  | Asset metadata: precision and dust threshold               |

Headers for the sample inputs are shown below.

`trades.csv`:
```
TradeId,ExecutionTimestamp,Pair,Base,Quote,Quantity,Price,QuoteValue,BuyMember,BuyNettingAccount,BuyPositionAccount,SellMember,SellNettingAccount,SellPositionAccount,ReportedTimestamp
```

`ledger.csv`:
```
Member,Tier,BTC,ETH,SOL,XRP,AAVE,BCH,LTC,LINK,UNI,PYTH,USD,USDC,EUR,GBP,JPY,RLUSD
```

`assets.csv`:
```
Asset,Dust Threshold,Precision
```

## Running the engine

The CLI takes a single optional argument — the path to the data folder. It defaults to `data`.

```bash
# Run against the default ./data folder
go run ./cmd/main

# Run against a specific dataset
go run ./cmd/main small-sample
```

Output files are written into the same folder:

- `settlement-instructions.csv` — net debits/credits per member+asset
- `trade-settlements.csv` — per-trade settlement classification (FULL / PARTIAL / DEFERRED)

## Validating output

After running the engine, validate the results against the input trades:

```bash
go run ./cmd/validator           # validates ./data
go run ./cmd/validator small-sample     # validates ./small-sample
```

The validator checks:
1. Net trade movements reconcile with the issued settlement instructions
2. Strict FIFO ordering — once a trade is partial/deferred, all later trades for the same member+asset position must also be deferred

It exits with a non-zero status if any check fails.

## Build

```bash
# Engine binary
go build -o settlement ./cmd/main

# Validator binary
go build -o validator ./cmd/validator
```

Then:

```bash
./settlement data
./validator data
```

## Test

```bash
go test ./...
```

## Common workflow

```bash
go run ./cmd/main L06_Exactly_funded && go run ./cmd/validator L06_Exactly_funded
```

This generates instructions for `data3/` and immediately validates them.
