# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Go module `settlement` (Go 1.26) — a multilateral net settlement engine. Given trades,
opening ledger balances, and per-asset constraints, it nets positions and produces the
minimum ledger movements (debits/credits) that honour the trades while respecting members'
available balances. Uses `github.com/shopspring/decimal` for decimal arithmetic.

- `cmd/main/` — CLI: load CSVs → run engine → write output CSVs (with `loader/`, `output/`, `wappers/`)
- `cmd/validator/` — reconciles engine output and verifies strict-FIFO ordering
- `pkg/settlement/` — core engine: `engine.go` (netting + LIFO deficit unwinding), `batch.go`
  (partition into independent batches), `numeric.go` (fixed-point, scaled by 10^20), `settlement.go` (interfaces + constants)

## Commands

```bash
# Run the engine on a dataset under data/ (omit name for an interactive menu)
go run ./cmd/main [dataset-name]

# Validate the engine's output for that dataset
go run ./cmd/validator [dataset-name]

# Typical loop
go run ./cmd/main L06_Exactly_funded && go run ./cmd/validator L06_Exactly_funded

# Build
go build -o settlement ./cmd/main

# Test
go test ./...

# Single package test
go test ./path/to/pkg/...

# Lint (if golangci-lint is available)
golangci-lint run
```

## Data

- Input lives in `data/<dataset>/`: `trades.csv`, `ledger.csv`, `assets.csv`.
  `ledger.csv` is wide-format — one row per member, one column per asset symbol; empty cells mean "no balance".
- Output is written to `output/<dataset>/`: `settlement-instructions.csv`, `trade-settlements.csv`.
- `data/netting-*.csv` are intermediate matrix snapshots, not inputs.
- Algorithm/domain references: `Pseudo.md`, `confluence.md`, `README.md`.

## Conventions

- Follow standard Go project layout as the codebase grows (`cmd/`, `internal/`, `pkg/` as appropriate)
- Use `go mod tidy` after adding or removing dependencies
- Module path is the bare `settlement` (not a URL) — imports look like `settlement/cmd/main/loader`
- The main CLI hardcodes `StrictFifoMode_Member_Instrument_CounterParty`; changing the FIFO mode requires a code edit
