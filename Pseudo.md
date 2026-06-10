# settlement package

Multilateral trade settlement engine. Given a set of trades, member ledger
balances, and asset reference data, it produces settlement instructions
(debits/credits) and classifies each trade as fully settled, partially settled,
or deferred. Per-asset precision and dust thresholds are honoured throughout.

---

## Pseudo Code

### Entry Point

```
settlement.GenerateInstructions(trades, ledger, assets, strictFifo):
    engine = newEngine(strictFifo)
    engine.init(trades, ledger, assets)
    return engine.run()
```

`assets` provides each asset's symbol, decimal precision, and dust threshold.

---

### Initialisation (`engine.init`)

```
init(trades, ledger, assets):
    sort trades by execution time (ascending)
    build assetMap[symbol] -> Asset (for precision + dust lookup)

    for each trade:
        assign integer IDs to buyer, seller, baseAsset, quoteAsset
        look up base/quote precision and dust threshold from assetMap
        quantity      = roundDown(trade.quantity, basePrecision)
        price         = trade.price
        quoteQuantity = roundDown(quantity × price, quotePrecision)

        store trade with:
            BaseQuantity, QuoteQuantity      (the rounded order amounts)
            RemainingBaseQty  = BaseQuantity (settled-so-far tracker)
            RemainingQuoteQty = QuoteQuantity
            BaseDust, QuoteDust              (per-asset dust thresholds)
            BasePrecision, QuotePrecision

    for each ledger entry:
        ledger[member][asset]  = roundDown(balance, assetPrecision)
        netting[member][asset] = ledger[member][asset]   // working copy

    build assetLiability[member][asset] = ordered list of trades
        where member is the buyer  -> liability is quoteAsset (they owe quote)
        where member is the seller -> liability is baseAsset  (they owe base)

    for each assetLiability list:
        set Start pointer to last trade (newest, for LIFO unwinding)
```

`RemainingBaseQty` / `RemainingQuoteQty` track how much of each trade is still
intended to settle. Reversals decrement them; classification later inspects
them.

---

### Netting (`engine.calculateNetting`)

Apply every trade's remaining quantity to the netting matrix:

```
for each trade:
    netting[buyer][baseAsset]   += RemainingBaseQty   // buyer receives base
    netting[buyer][quoteAsset]  -= RemainingQuoteQty  // buyer pays quote
    netting[seller][baseAsset]  -= RemainingBaseQty   // seller delivers base
    netting[seller][quoteAsset] += RemainingQuoteQty  // seller receives quote
```

After this step, any negative `netting[member][asset]` means that member cannot
cover their obligation with their opening balance plus what they receive from
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
            trd = assetLiability[member][asset].Trades[Start]   // newest first

            if trd.RemainingBaseQty == 0:
                Start -= 1   // already fully reversed, skip
                continue

            if asset == trd.baseAsset:
                qtyToReverse = min(remaining, trd.RemainingBaseQty)
                reverseTrade(trd, qtyToReverse, base=true)
            else:  // asset == trd.quoteAsset
                qtyToReverse = min(remaining, trd.RemainingQuoteQty)
                reverseTrade(trd, qtyToReverse, base=false)

            remaining -= qtyToReverse
            if trade fully consumed on the driving side: Start -= 1

    if strictFifo:
        deferFollowingTradesIfNotFullySettled()   // FIFO unwind (see below)
    return true  // caller will re-run
```

#### reverseTrade

`reverseTrade` partially (or fully) unwinds a trade. The driver side (base or
quote) is specified; the other side is derived from the trade's ratio. Per-asset
precision and dust rules are then applied.

```
reverseTrade(trd, qty, base):
    if base:
        baseQty  = qty
        quoteQty = qty × trd.QuoteQuantity / trd.BaseQuantity
    else:
        quoteQty = qty
        baseQty  = qty × trd.BaseQuantity / trd.QuoteQuantity

    // Snap up to each asset's precision so reversal can't leave
    // fractional digits beyond what the asset supports.
    baseQty  = roundUp(baseQty,  trd.BasePrecision)
    quoteQty = roundUp(quoteQty, trd.QuotePrecision)

    // Guard: if the non-driving side rounded to zero, bump to one precision step
    // so it still decrements RemainingQty.
    if baseQty  == 0: baseQty  = 10^(-trd.BasePrecision)
    if quoteQty == 0: quoteQty = 10^(-trd.QuotePrecision)

    (baseQty, quoteQty) = applyDustRules(trd, baseQty, quoteQty)

    // Never reverse more than what's left on the trade.
    baseQty  = min(baseQty,  trd.RemainingBaseQty)
    quoteQty = min(quoteQty, trd.RemainingQuoteQty)

    trd.RemainingBaseQty  -= baseQty
    trd.RemainingQuoteQty -= quoteQty

    // Undo the netting effect for the reversed portion.
    netting[buyer][baseAsset]   -= baseQty
    netting[seller][baseAsset]  += baseQty
    netting[buyer][quoteAsset]  += quoteQty
    netting[seller][quoteAsset] -= quoteQty
```

#### applyDustRules

After a candidate `(baseQty, quoteQty)` is computed, the dust contract is
enforced:

```
applyDustRules(trd, baseQty, quoteQty):
    fullBase  = trd.RemainingBaseQty
    fullQuote = trd.RemainingQuoteQty

    repeat up to 2 times:
        newSettledBase   = trd.RemainingBaseQty  - baseQty   // what would remain settled
        newSettledQuote  = trd.RemainingQuoteQty - quoteQty
        newDeferredBase  = trd.BaseQuantity  - newSettledBase
        newDeferredQuote = trd.QuoteQuantity - newSettledQuote

        // (1) Settled side sub-dust -> fully reverse the trade
        if newSettledBase  is below trd.BaseDust  OR
           newSettledQuote is below trd.QuoteDust:
            return (fullBase, fullQuote)

        // (2) Deferred side clears dust on both sides -> accept current reversal
        if newDeferredBase  is at/above trd.BaseDust  AND
           newDeferredQuote is at/above trd.QuoteDust:
            return (baseQty, quoteQty)

        // (3) Deferred side sub-dust: expand the reversal so cumulative deferral
        //     reaches the dust threshold on the offending side. Pick the binding
        //     side, derive the other from the trade ratio, snap up to precision,
        //     and loop to revalidate.
        needBase  = max(trd.BaseDust  - alreadyDeferredBase,  baseQty)
        needQuote = max(trd.QuoteDust - alreadyDeferredQuote, quoteQty)
        if (needBase × trd.QuoteQuantity / trd.BaseQuantity) >= needQuote:
            baseQty  = roundUp(needBase, trd.BasePrecision)
            quoteQty = roundUp(needBase × trd.QuoteQuantity / trd.BaseQuantity,
                               trd.QuotePrecision)
        else:
            quoteQty = roundUp(needQuote, trd.QuotePrecision)
            baseQty  = roundUp(needQuote × trd.BaseQuantity / trd.QuoteQuantity,
                               trd.BasePrecision)

        // If expansion would overrun what's left -> full reversal.
        if baseQty >= fullBase OR quoteQty >= fullQuote:
            return (fullBase, fullQuote)

    return (fullBase, fullQuote)   // defensive fallback
```

Outcome: every reversal either leaves both sides cleanly above dust or
collapses to a full reversal — there are no sub-dust residuals.

#### deferFollowingTradesIfNotFullySettled (per-iteration FIFO unwind)

Called at the end of every `runIteration`. Any trade that follows a
non-fully-settled trade for the same buyer-quote or seller-base position is
fully reversed.

```
repeat until no change:
    deferred = {}   // set of "member-asset" keys blocked by an earlier partial

    for each trade (in original execution-time order):
        buyerKey  = (buyer,  quoteAsset)
        sellerKey = (seller, baseAsset)

        if buyerKey in deferred OR sellerKey in deferred:
            mark both keys as deferred
            if trade still has remaining qty:
                reverseTrade(trade, RemainingBaseQty, base=true)   // full unwind
            continue

        if BaseQuantity != RemainingBaseQty:    // trade is partial
            add buyerKey and sellerKey to deferred
```

---

### Convergence Loop (`engine.run`)

```
run():
    calculateNetting()

    loop:
        if runIteration() == false: break   // no negatives left
```

When the loop exits, `RemainingBaseQty` / `RemainingQuoteQty` on each trade
reflect the post-resolution settled portions.

---

### Trade Result Classification

After convergence, the netting matrix is reset to opening ledger balances and
each trade is classified. Dust rules apply here too:

```
for each trade:
    deferredBase  = BaseQuantity  - RemainingBaseQty
    deferredQuote = QuoteQuantity - RemainingQuoteQty

    if RemainingBaseQty == 0 OR RemainingQuoteQty == 0
        OR RemainingBaseQty  is below BaseDust
        OR RemainingQuoteQty is below QuoteDust:
        // Settled side too small -> defer the whole trade
        RemainingBaseQty  = 0
        RemainingQuoteQty = 0
        status = DEFERRED

    else if (deferredBase  == 0 OR deferredBase  is below BaseDust)
        AND (deferredQuote == 0 OR deferredQuote is below QuoteDust):
        // Deferred side too small on both sides -> settle in full
        RemainingBaseQty  = BaseQuantity
        RemainingQuoteQty = QuoteQuantity
        status = FULL

    else:
        status = PARTIAL

    SettledQuantity       = RemainingBaseQty
    SettledQuoteQuantity  = RemainingQuoteQty
    DeferredQuantity      = BaseQuantity  - RemainingBaseQty
    DeferredQuoteQuantity = QuoteQuantity - RemainingQuoteQty
```

---

### Instruction Generation

After classification the netting matrix is recomputed from the (now-final)
remaining quantities. Per-member-asset diffs become settlement instructions:

```
calculateNetting()   // from updated RemainingBaseQty / RemainingQuoteQty

for each (member, asset):
    diff    = netting[member][asset] - ledger[member][asset]
    absDiff = roundDown(abs(diff), asset.precision)

    if absDiff == 0: skip   // negligible / sub-precision

    direction = IN  if diff > 0 else OUT

    emit Instruction{
        Member:         member,
        Asset:          asset,
        OpeningBalance: ledger[member][asset],
        NetAmount:      absDiff,
        ClosingBalance: netting[member][asset],
        Direction:      direction,
    }
```

`IN` adds assets to the member's ledger; `OUT` removes them.

---

### Batch Splitting (`splitBatches`)

Trades classified as `DEFERRED` are separated out. The remaining settled trades
and instructions are grouped into independent batches via a union-find over
`(member, asset)` keys:

```
separate trades into: settled, deferred

// Build connectivity graph
for each settled trade:
    union(buyer+baseAsset,  buyer+quoteAsset)
    union(buyer+baseAsset,  seller+baseAsset)
    union(buyer+baseAsset,  seller+quoteAsset)

for each instruction:
    ensure(member+asset)   // standalone instructions get their own roots

// Group by connected component
tradesByRoot = group settled trades by root(buyer+baseAsset)
instrByRoot  = group instructions  by root(member+asset)

batches = [ Result{trades, instructions} for each root, sorted by root ]

return Results{ Batches: batches, Deferred: deferred }
```

Independent batches share no member-asset key and can be settled atomically and
independently.

---

### Numeric Representation

All quantities are stored as scaled `big.Int` values with `scaleDigits = 20`
decimal places of precision (scale factor `10^20`). Conversion to/from
`decimal.Decimal` happens at the boundary.

```
fromDecimal(d)         = d × 10^20            (as big.Int)
toDecimal(n)           = n / 10^20            (as decimal.Decimal, rounded to 18)
multiply(a, b)         = (a × b) / 10^20
multiplyAndDivide(a, num, denom) = (a × num) / denom
precisionStep(p)       = 10^(20 - p)          (one unit at asset precision p)
roundToPrecision(v, p) = snap v to a multiple of precisionStep(p), down or up
isBelowDust(v, dust)   = (0 < v < dust)       (exact zero is not dust)
```

Per-asset precision controls how aggressively values are snapped; dust
thresholds drive whether a sub-precision remainder is considered settleable.
