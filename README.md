# settlement package

Multilateral trade settlement engine. Given a set of trades and member ledger balances, it produces settlement instructions (debits/credits) and classifies each trade as fully settled, partially settled, or deferred.

---

## Pseudo Code

### Entry Point

```
settlement.GenerateInstructions(trades, ledger, strictFifo):
    engine = newEngine(strictFifo)
    engine.init(trades, ledger)
    return engine.run()
```

---

### Initialisation (`engine.init`)

```
init(trades, ledger):
    sort trades by execution time (ascending)

    for each trade:
        assign integer IDs to buyer, seller, baseAsset, quoteAsset
        compute quoteQuantity = quantity × price
        store trade with RemainingBaseQty = quantity, RemainingQuoteQty = quoteQuantity

    build ledger[member][asset]  = opening balance from ledger entries
    build netting[member][asset] = copy of ledger (will be mutated)

    build assetLiability[member][asset] = ordered list of trades
        where member is the buyer  → liability is quoteAsset (they owe quote)
        where member is the seller → liability is baseAsset  (they owe base)

    for each assetLiability list:
        set Start pointer to last trade (newest, for LIFO unwinding)
```

---

### Netting (`engine.calculateNetting`)

Apply every trade's full quantity to the netting matrix:

```
for each trade:
    netting[buyer][baseAsset]   += baseQuantity   // buyer receives base
    netting[buyer][quoteAsset]  -= quoteQuantity  // buyer pays quote
    netting[seller][baseAsset]  -= baseQuantity   // seller delivers base
    netting[seller][quoteAsset] += quoteQuantity  // seller receives quote
```

After this step, any negative `netting[member][asset]` means that member cannot
cover their obligation with their current balance plus what they receive from
other trades.

---

### Iterative Deficit Resolution (`engine.runIteration`)

Run repeatedly until no negatives remain:

```
runIteration():
    negatives = [ (member, asset) for all netting[member][asset] < 0 ]

    if negatives is empty:
        return false  // done

    for each (member, asset) in negatives:
        remaining = abs(netting[member][asset])   // deficit to eliminate

        while remaining > 0:
            trd = assetLiability[member][asset].Trades[Start]   // newest trade first

            if trd.RemainingQty == 0:
                Start -= 1   // trade already fully reversed, skip
                continue

            // Partially or fully reverse this trade to recover the deficit
            if asset == trd.baseAsset:
                qtyToReverse = min(remaining, trd.RemainingBaseQty)
                reverseTrade(trd, qtyToReverse, base=true)
            else:  // asset == trd.quoteAsset
                qtyToReverse = min(remaining, trd.RemainingQuoteQty)
                reverseTrade(trd, qtyToReverse, base=false)

            remaining -= qtyToReverse
            if trade fully consumed: Start -= 1

    return true  // negatives existed; caller will re-run
```

#### reverseTrade

```
reverseTrade(trd, qty, base):
    if base:
        baseQty  = qty
        quoteQty = qty × trd.QuoteQuantity / trd.BaseQuantity
    else:
        quoteQty = qty
        baseQty  = qty × trd.BaseQuantity / trd.QuoteQuantity

    trd.RemainingBaseQty  -= baseQty
    trd.RemainingQuoteQty -= quoteQty

    // undo the netting effect for the reversed portion
    netting[buyer][baseAsset]   -= baseQty
    netting[seller][baseAsset]  += baseQty
    netting[buyer][quoteAsset]  += quoteQty
    netting[seller][quoteAsset] -= quoteQty
```

---

### Trade Result Classification (`engine.run`)

After iterations converge:

```
for each trade:
    if RemainingBaseQty == 0:
        status = DEFERRED     // fully reversed; nothing settles
    elif RemainingBaseQty == BaseQuantity:
        status = FULL         // untouched; fully settles
    else:
        status = PARTIAL      // partially reversed

    settledQuantity       = RemainingBaseQty
    deferredQuantity      = BaseQuantity - RemainingBaseQty
    (same ratio for quote quantities)
```

---

### Strict FIFO Enforcement (`deferFollowingTradesIfNotFullySettled`)

When `strictFifo = true`, applied after initial classification (looped until stable):

```
reset netting to opening ledger balances

deferred = {}   // set of "member-asset" keys that are blocked

for each trade (in original execution-time order):
    buyerKey  = buyer  + quoteAsset
    sellerKey = seller + baseAsset

    if buyerKey in deferred OR sellerKey in deferred:
        // This trade is behind an unsettled one — force-defer it
        mark trade as DEFERRED (settledQty = 0, deferredQty = full)
        add buyerKey and sellerKey to deferred
        continue

    if trade is not FULL:
        // This trade itself is a blocker — all later trades for these
        // member-asset pairs must be deferred too
        add buyerKey and sellerKey to deferred

    if trade is not DEFERRED:
        // Apply its settled amounts to the running netting
        netting[buyer][baseAsset]   += settledQuantity
        netting[seller][quoteAsset] += settledQuoteQuantity
        netting[buyer][quoteAsset]  -= settledQuoteQuantity
        netting[seller][baseAsset]  -= settledQuantity
```

Repeat until a full pass produces no changes.

---

### Instruction Generation

```
for each (member, asset):
    diff = netting[member][asset] - ledger[member][asset]

    if diff == 0: skip

    direction = CREDIT if diff > 0 else DEBIT

    emit Instruction{
        Member:         member,
        Asset:          asset,
        OpeningBalance: ledger[member][asset],
        NetAmount:      abs(diff),
        ClosingBalance: netting[member][asset],
        Direction:      direction,
    }
```

---

### Batch Splitting (`splitBatches`)

Trades that are DEFERRED are separated out. The remaining settled trades and
instructions are grouped into independent batches using a union-find structure:

```
separate trades into: settled, deferred

// Build connectivity graph
for each settled trade:
    union(buyer+baseAsset, buyer+quoteAsset)
    union(buyer+baseAsset, seller+baseAsset)
    union(buyer+baseAsset, seller+quoteAsset)

// Group by connected component (root)
tradesByRoot    = group settled trades by find(buyer+baseAsset)
instrByRoot     = group instructions by find(member+asset)

// Each root is an independent batch
batches = [ Result{trades, instructions} for each root ]

return Results{ Batches: batches, Deferred: deferred }
```

Independent batches can be settled atomically without affecting one another.

---

### Numeric Representation

All quantities are stored as scaled integers (`big.Int`) with 6 decimal places
of precision (scale factor = 1,000,000). Conversion from/to `decimal.Decimal`
happens at the boundary.

```
fromDecimal(d) = d × 1_000_000  (as big.Int)
toDecimal(n)   = n / 1_000_000  (as decimal.Decimal)
multiply(a, b) = (a × b) / 1_000_000
```
