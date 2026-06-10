# Settlement Engine — Complete Reference

This document is the long-form reference for the multilateral settlement
engine. It walks every step of the algorithm using the `small-sample`
dataset, explains the reasoning behind each rule, and documents the
penalty logic. It is intentionally written so that a business reader,
operator, or new engineer can read it cold and understand what the
engine does, why, and what the output means.

---

## Table of Contents

1.  [What the engine does](#1-what-the-engine-does)
2.  [The `small-sample` dataset](#2-the-small-sample-dataset)
3.  [Pipeline at a glance](#3-pipeline-at-a-glance)
4.  [Step 1 · Net everything](#4-step-1--net-everything)
5.  [Penalty logic](#5-penalty-logic)
6.  [Step 2 · Resolve deficits (LIFO unwinding)](#6-step-2--resolve-deficits-lifo-unwinding)
7.  [Step 3 · Dust threshold rules](#7-step-3--dust-threshold-rules)
8.  [Step 4 · Strict FIFO ordering](#8-step-4--strict-fifo-ordering)
9.  [Step 5 · Convergence loop](#9-step-5--convergence-loop)
10. [Step 6 · Classify each trade](#10-step-6--classify-each-trade)
11. [Step 7 · Split into independent batches](#11-step-7--split-into-independent-batches)
12. [Step 8 · Emit settlement instructions](#12-step-8--emit-settlement-instructions)
13. [Numeric precision](#13-numeric-precision)
14. [Why this design](#14-why-this-design)
15. [Glossary](#15-glossary)

---

## 1. What the Engine Does

At the end of every settlement window the engine is given three inputs:

| Input | What it is |
|-------|-----------|
| **Trades** (`trades.csv`) | Every executed trade in the window. Each trade has two sides — a buyer and a seller — and one trading pair (e.g. `BTC-ETH`). |
| **Ledger** (`ledger.csv`) | Each member's opening balance for every asset. |
| **Assets** (`assets.csv`) | Per-asset metadata: number of decimal places (precision) and a dust threshold below which an amount is uneconomical to move. |

From these it produces:

| Output | What it is |
|--------|-----------|
| **Trade settlements** (`trade-settlements.csv`) | Each trade classified as `FULL`, `PARTIAL`, or `DEFERRED`, with the settled and deferred portions. |
| **Settlement instructions** (`settlement-instructions.csv`) | The minimum set of `IN` / `OUT` ledger movements needed to make everyone's closing balance match. |
| **Independent batches** | The instructions are grouped into batches that share no `(member, asset)` key — so they can be executed atomically in parallel. |
| **Penalty data** | Per-member penalty information for the **initial deficit set only** — see [§5](#5-penalty-logic). |

The engine honours four rules at every step:

1.  **LIFO unwinding** — when a member is short of an asset, reverse the
    *newest* trade for that liability position first.
2.  **Strict FIFO settlement** — once an earlier trade for a
    `(member, asset)` position is not honoured in full, every later trade
    for that same position must also be deferred. Commitments can't
    queue-jump.
3.  **Dust safety** — no settlement leg ever emits a sub-dust amount.
    Either both sides clear dust, or the leg goes to zero.
4.  **Penalty fairness** — only the initial deficit set (the deficits
    present *after the first netting pass*) generates penalty. Anything
    new that appears later as cascade fallout is not penalized.

---

## 2. The `small-sample` Dataset

### 2.1 Members and opening balances (`ledger.csv`)

| Member  | Tier | BTC | ETH | XRP | ADA | BNB | USDT |
|---------|------|----:|----:|----:|----:|----:|-----:|
| Alice   | T1   |   8 |  50 |  40 |  20 | 200 |  100 |
| Bob     | T1   |   0 |  55 |   – |  50 |   – |    0 |
| Charlie | T1   |   5 |  25 |  20 |   – |   – |  200 |
| Dave    | T2   |   2 |   – |  12 |  10 |  50 |  200 |
| Eve     | T2   |  15 |   5 |  20 |   8 | 200 |  100 |
| Frank   | T2   |  15 |   1 |  50 |   0 | 150 |    0 |

A dash means the member holds none of that asset.

### 2.2 Trades (`trades.csv`) — blotter view

| #  | Time         | Pair      | Quantity | Price       | Notional | Buyer   | Seller  |
|----|--------------|-----------|---------:|-------------|---------:|---------|---------|
| 1  | 08:00:00.060 | BTC-ETH   |  10 BTC  | 5 ETH/BTC   |   50 ETH | Alice   | Bob     |
| 2  | 08:00:00.258 | BTC-ETH   |   5 BTC  | 5 ETH/BTC   |   25 ETH | Charlie | Dave    |
| 3  | 08:00:00.901 | XRP-USDT  |  20 XRP  | 10 USDT/XRP |  200 USDT| Eve     | Charlie |
| 4  | 08:00:00.911 | ADA-BNB   |  10 ADA  | 5 BNB/ADA   |   50 BNB | Dave    | Eve     |
| 5  | 08:00:00.931 | ETH-XRP   |  25 ETH  | 2 XRP/ETH   |   50 XRP | Frank   | Dave    |
| 6  | 08:00:01.065 | ETH-USDT  |  10 ETH  | 20 USDT/ETH |  200 USDT| Bob     | Eve     |
| 7  | 08:00:01.409 | XRP-USDT  |  10 XRP  | 10 USDT/XRP |  100 USDT| Dave    | Alice   |
| 8  | 08:00:01.496 | ADA-BNB   |  20 ADA  | 5 BNB/ADA   |  100 BNB | Alice   | Dave    |
| 9  | 08:00:01.496 | ADA-USDT  |   2 ADA  | 50 USDT/ADA |  100 USDT| Eve     | Bob     |
| 10 | 08:00:01.726 | BTC-BNB   |  15 BTC  | 10 BNB/BTC  |  150 BNB | Eve     | Frank   |

**The buyer** pays the notional (`quantity × price` in quote currency)
and receives the quantity in the base currency. **The seller** does the
opposite. Order matters: time-priority drives both LIFO unwinding (newest
first) and strict FIFO settlement (earliest commitment first).

### 2.3 Asset rules (`assets.csv`)

| Asset | Precision (decimals) | Dust threshold |
|-------|---------------------:|---------------:|
| BTC   |                    8 |     0.00000294 |
| ETH   |                   18 |          1e-16 |
| XRP   |                    6 |         0.0001 |
| ADA   |                    6 |              1 |
| BNB   |                   18 |          1e-16 |
| USDT  |                    6 |           0.01 |

**Precision** is the maximum number of decimals an amount can carry.
**Dust threshold** is the smallest amount worth moving. Anything in
`(0, dust)` is sub-dust and must not appear on a settlement leg.

---

## 3. Pipeline at a Glance

```mermaid
flowchart TD
    A[Load trades, ledger, assets] --> B[Sort trades by execution time]
    B --> C[Round quantities to asset precision]
    C --> D[Build liability index<br/>per member+asset]
    D --> E[Step 1: Net every trade into<br/>member × asset matrix]
    E --> P{Identify initial<br/>deficit set<br/>→ penalty}
    P --> F{Any negative<br/>net position?}
    F -- Yes --> G[Step 2: Reverse newest trade LIFO<br/>for each deficit]
    G --> H[Step 3: Dust check on every reversal]
    H --> I[Step 4: Strict FIFO knock-on<br/>defer trades following partials]
    I --> E
    F -- No --> J[Step 6: Classify each trade<br/>FULL / PARTIAL / DEFERRED]
    J --> K[Step 7: Union-find on<br/>member+asset → batches]
    K --> L[Step 8: Emit IN/OUT instructions]
    L --> M[Write CSV output<br/>+ deferred trades for next window]
```

The loop between netting (E) and resolution (G–I) is the heart of the
engine. On `small-sample` it converges in **5 iterations**.

The branch from E to P shows where the **penalty set** is captured —
it's frozen at the *first* netting pass and never changes afterwards,
even though the netting matrix is recomputed many times during
convergence.

---

## 4. Step 1 — Net Everything

### 4.1 What netting means

The engine pretends, for a moment, that every trade settles in full. For
each trade it applies four flows to the `member × asset` matrix:

```mermaid
flowchart LR
    subgraph T[" Trade #1: Alice buys 10 BTC from Bob, paying 50 ETH "]
        direction LR
        Alice((Alice))
        Bob((Bob))
        Alice -- "+10 BTC (gain)" --> ABT[Alice·BTC]
        Alice -- "−50 ETH (loss)" --> AET[Alice·ETH]
        Bob   -- "−10 BTC (loss)" --> BBT[Bob·BTC]
        Bob   -- "+50 ETH (gain)" --> BET[Bob·ETH]
    end
```

| Side   | Base asset | Quote asset |
|--------|-----------|-------------|
| Buyer  | gain (↑)  | loss (↓)    |
| Seller | loss (↓)  | gain (↑)    |

The four flows always sum to zero per asset — settlement *moves* value,
it doesn't create or destroy it.

### 4.2 The `small-sample` netting result

Summing all flows on top of the opening ledger:

| Member  | BTC      | ETH     | XRP | ADA | BNB | USDT       |
|---------|---------:|--------:|----:|----:|----:|-----------:|
| Alice   |       18 |       0 |  30 |  40 | 100 |        200 |
| Bob     | **−10** ⚠ |     115 |   – |  48 |   – | **−100** ⚠ |
| Charlie |       10 |       0 |   0 |   – |   – |        400 |
| Dave    |  **−3** ⚠ |       0 |  72 |   0 | 100 |        100 |
| Eve     |       30 |  **−5** ⚠ |  40 |   0 | 100 |          0 |
| Frank   |        0 |      26 |   0 |   0 | 300 |          0 |

The bolded cells are **deficits** — the member has agreed to deliver
more than they have (after netting). There are four:

| # | Member · Asset | Deficit  | Caused by                                  |
|---|----------------|---------:|--------------------------------------------|
| 1 | `Bob · BTC`    | −10 BTC  | T1 sells 10 BTC; Bob owns 0.                |
| 2 | `Bob · USDT`   | −100 USDT| T6 pays 200; T9 receives 100; opening 0.    |
| 3 | `Dave · BTC`   | −3 BTC   | T2 sells 5 BTC; Dave owns 2.                |
| 4 | `Eve · ETH`    | −5 ETH   | T6 sells 10 ETH; Eve owns 5.                |

### 4.3 Why net first

A member may be short on one trade but *receive* the same asset on
another trade in the same window. Without netting, the engine would
reverse trades unnecessarily. With netting, only **true** uncovered
positions get resolved.

For example, Eve's USDT row in the netting matrix is 0:
`100 (opening) − 200 (T3) + 200 (T6) − 100 (T9) = 0`. If we'd looked at
each trade individually, T3 alone would have looked like a 100-USDT
shortfall. Netting reveals that the inflows cover it.

---

## 5. Penalty Logic

This is one of the most important rules, and the most counter-intuitive
to read out of the code, so it gets its own section.

### 5.1 The rule

> **Only the initial deficit set is penalized.**
> The "initial deficit set" is the set of `(member, asset)` cells whose
> net position is negative after the **first** netting pass (i.e.
> immediately after [§4](#4-step-1--net-everything), before any
> reversal happens). Anything that becomes negative later — as a
> consequence of the engine's own reversal cascade — is treated as
> cascade fallout and is **not** penalized.

### 5.2 Why

The engine resolves deficits by reversing earlier trades. Reversing a
trade un-applies its four flows — which can push *other* cells negative.
Those new negatives weren't caused by the member's behaviour; they were
caused by the engine's resolution mechanism. Charging a penalty for them
would double-bill the same underlying shortfall.

```mermaid
flowchart TD
    A[Initial netting matrix] --> B{Any cell negative?}
    B -- Yes --> C[Record cell in INITIAL_DEFICITS<br/>frozen — never updated]
    B --> D[Resolve via LIFO reversal]
    D --> E{Reversal pushed<br/>other cells negative?}
    E -- Yes --> F[These are CASCADE FALLOUT<br/>NOT added to INITIAL_DEFICITS]
    E -- No --> G[Continue]
    F --> D
    C --> H[Compute penalty against<br/>INITIAL_DEFICITS only]
    G --> H
```

### 5.3 In `small-sample`

| Cell           | Status                        | Penalized?            |
|----------------|-------------------------------|-----------------------|
| `Bob · BTC`    | Initial deficit (−10)         | ✅ Yes                 |
| `Bob · USDT`   | Initial deficit (−100)        | ✅ Yes                 |
| `Dave · BTC`   | Initial deficit (−3)          | ✅ Yes                 |
| `Eve · ETH`    | Initial deficit (−5)          | ✅ Yes                 |
| `Dave · ETH`   | Introduced when T2 partial reversed pulled 15 ETH off Dave's incoming flow | ❌ No (cascade fallout) |
| `Charlie · ETH`| Introduced when T2 partial reversed | ❌ No (cascade fallout) |
| any other negative that appears mid-convergence | – | ❌ No |

### 5.4 Reading example

When Dave's `−3 BTC` deficit is resolved, the engine reverses 3 BTC /
15 ETH of T2. That reversal takes 15 ETH off Charlie's incoming flow
*and* 15 ETH off Dave's incoming flow — Dave's ETH cell drops to −15.
Even though that's a new red cell on the matrix, **the penalty engine
does not see it**. Dave is still only penalized for the original
`Dave · BTC = −3` deficit.

> *"Initial deficits are commitments. Cascade fallout is plumbing.
> Members pay for commitments they couldn't meet, not for the engine's
> way of cleaning up."*

---

## 6. Step 2 — Resolve Deficits (LIFO Unwinding)

### 6.1 The newest-first rule

For every deficit, the engine reverses the **most recent** trade that
put the member on the owing side of that asset, walking backward until
the deficit is cleared.

```mermaid
flowchart TD
    Start[Deficit found:<br/>member M is short of asset A by X] --> Idx[Look up liability index<br/>trades where M owes A,<br/>ordered by execution time]
    Idx --> Pick[Pick newest unreversed trade T]
    Pick --> Drv{Which side<br/>of T owes A?}
    Drv -- base --> Rb[qtyToReverse = min X, T.remainingBase]
    Drv -- quote --> Rq[qtyToReverse = min X, T.remainingQuote]
    Rb --> Scale[Scale OTHER side by trade ratio<br/>quote = qty × T.quoteQty / T.baseQty]
    Rq --> Scale2[Scale OTHER side by trade ratio<br/>base = qty × T.baseQty / T.quoteQty]
    Scale --> Snap[Round up both sides to asset precision]
    Scale2 --> Snap
    Snap --> Dust[Apply dust rules — see Step 3]
    Dust --> Apply[Subtract from T.remaining<br/>and undo netting impact]
    Apply --> Q{Still short?}
    Q -- Yes --> Pick
    Q -- No --> Done[Deficit cleared]
```

**Why newest first?** Markets honour earlier commitments before later
ones. Reversing the newest trade preserves the fairness of earlier
trades — they keep their fills, only the latest gets walked back.

### 6.2 Reversals preserve the trade ratio

If we reverse `q` units of the base side, the corresponding quote
reversal is:

```
quoteReversed = q × (trade.quoteQty / trade.baseQty)
```

This keeps the partially-settled remainder at exactly the original
agreed price. The remaining settled portion is a smaller version of the
original trade, not a re-priced one.

### 6.3 Worked example A — Bob's BTC (full reversal)

Bob is short 10 BTC. His only BTC liability is T1 (he sells 10 BTC).
The deficit equals the full base side, so T1 is reversed in full:

| | Base | Quote |
|--|------|-------|
| Original T1 | 10 BTC | 50 ETH |
| Reverse fully | −10 BTC | −50 ETH |
| **Remains settled** | **0 BTC** | **0 ETH** |

**Result:** T1 → `DEFERRED`. The full 10 BTC ↔ 50 ETH rolls to the
next window. Bob's BTC deficit cleared. Alice's incoming BTC drops by
10, her outgoing ETH returns by 50.

### 6.4 Worked example B — Dave's BTC (partial reversal + cascade)

Dave is short only 3 of the 5 BTC he agreed to deliver in T2.

| | Base | Quote |
|--|------|-------|
| Original T2 | 5 BTC | 25 ETH |
| Reverse 3 BTC | 3 BTC | 3 × (25 / 5) = 15 ETH |
| **Remains settled** | **2 BTC** | **10 ETH** |
| Deferred (rolls forward) | 3 BTC | 15 ETH |

**Result:** T2 → `PARTIAL`. Dave delivers 2 BTC instead of 5, Charlie
pays 10 ETH instead of 25.

**Cascade:** Reversing 15 ETH of T2's quote side takes 15 ETH off
*Dave's* incoming flow too (Dave was the seller, receiving ETH). His
ETH cell, previously 0, becomes −15.

> 🛈 This new `Dave · ETH = −15` is **cascade fallout**, not part of
> the initial deficit set — it is **not penalized** (see [§5](#5-penalty-logic)).
> The engine will iterate again to clear it, but Dave is only penalized
> for his original BTC shortfall.

### 6.5 Other reversals in `small-sample`

| Initial deficit | Reversed trade | Outcome                                    |
|-----------------|----------------|--------------------------------------------|
| `Bob · BTC = −10` | T1 (newest BTC liability) | T1 fully → `DEFERRED`                  |
| `Dave · BTC = −3` | T2 (newest BTC liability) | T2 partial 3/15 → `PARTIAL`            |
| `Eve · ETH = −5`  | T6 (newest ETH liability) | T6 fully → `DEFERRED` (dust-driven)    |
| `Bob · USDT = −100`| T9 (newest USDT liability)| T9 fully → `DEFERRED` (also FIFO-dragged) |

Each of these triggers further netting changes, which the convergence
loop sorts out in subsequent iterations (see [§9](#9-step-5--convergence-loop)).

---

## 7. Step 3 — Dust Threshold Rules

### 7.1 The visual model

Think of a trade as a horizontal log with **dust zones at each end**:

```
                              cut (split position)
                                       │
            ┌──────┬────────────────────────────────┬──────┐
   BTC      │ dust │   safe (above dust threshold)  │ dust │
            └──────┴────────────────────────────────┴──────┘
            ┌─────────────┬─────────────────────┬─────────────┐
   USDT     │    dust     │       safe          │    dust     │
            └─────────────┴─────────────────────┴─────────────┘
                                       │
                                       ✂
```

- The **width of each bar** is the trade's total quantity.
- The **left dust zone** is "if settled is smaller than this, settled is sub-dust".
- The **right dust zone** is "if deferred is smaller than this, deferred is sub-dust".
- The two assets in a pair have **different dust widths** — USDT
  (dust = 0.01) is much wider than BTC (dust = 0.00000294). The wider
  one is the binding constraint.

The engine wants the cut to land in the **safe zone of both assets at
the same time** — the intersection of both safe regions.

### 7.2 Three scenarios

| Scenario | Where the cut wants to land | What the engine does | Outcome |
|----------|----------------------------|----------------------|---------|
| **OK · can split** | In the safe zone of both assets. | Accept the partial reversal as-is. | `PARTIAL` |
| **Settled too small → DEFER** | Inside the **left** dust zone (settled portion would be sub-dust). | The engine *can't* widen — making settled smaller pushes it toward zero. Fully reverse the trade. | `DEFERRED` |
| **Deferred too small → WIDEN** | Inside the **right** dust zone (deferred portion would be sub-dust). | Drag the cut **leftward** until the deferred side just clears dust. Re-scale the other side to keep the trade ratio. | `PARTIAL` (with the smallest dust-clearing deferral) |

### 7.3 Worked example A — Defer scenario

Hypothetical trade: Alice buys 1 BTC from Bob @ 10 USDT. Bob is short
0.9999 BTC.

| Step | Base | Quote |
|------|------|-------|
| Original | 1 BTC | 10 USDT |
| Reverse to cover deficit | 0.9999 BTC | 9.999 USDT |
| Would-be settled | 0.0001 BTC | 0.001 USDT |

Dust check on the settled side:

| Side | Settled | Dust limit | Verdict |
|------|--------:|-----------:|---------|
| BTC  | 0.0001 BTC | 0.00000294 | ✅ above dust |
| USDT | 0.001 USDT | 0.01       | ❌ sub-dust  |

USDT fails. **Engine fully reverses the trade → `DEFERRED`.** Settling
0.001 USDT crumbs costs more than it moves.

### 7.4 Worked example B — Widen scenario

Same trade. Bob is short only 0.0001 BTC.

| Step | Base | Quote |
|------|------|-------|
| Original | 1 BTC | 10 USDT |
| Reverse small slice | 0.0001 BTC | 0.001 USDT |
| Would-be deferred | 0.0001 BTC | 0.001 USDT |

Dust check on the deferred side:

| Side | Deferred | Dust limit | Verdict |
|------|---------:|-----------:|---------|
| BTC  | 0.0001 BTC | 0.00000294 | ✅ above dust |
| USDT | 0.001 USDT | 0.01       | ❌ sub-dust  |

USDT deferred would be sub-dust. **Engine widens the reversal until
deferred USDT ≥ 0.01:**

| Step | Base | Quote |
|------|------|-------|
| Bind deferred USDT to dust limit | – | 0.01 USDT |
| Derive base · 0.01 / 10 | 0.001 BTC | – |
| **Adjusted settled** | **0.999 BTC** | **9.99 USDT** |
| Adjusted deferred | 0.001 BTC | 0.01 USDT |

Now both deferred legs clear dust. Trade settles as `PARTIAL`.

### 7.5 Why dust matters

Moving 0.001 USDT costs more in network fees and operational overhead
than the value moved. Sub-dust legs are pure noise. The engine
guarantees that every leg it emits is either exactly zero or strictly
above dust.

---

## 8. Step 4 — Strict FIFO Ordering

### 8.1 The rule

> Once an earlier trade for a `(member, asset)` *liability position* is
> not honoured in full, every later trade for that same position must
> be fully deferred — even if the member could individually cover it.

A trade has two liability positions:

| Key                           | Owed by                       |
|-------------------------------|-------------------------------|
| `(buyer, quote-asset)`        | Buyer owes the quote currency |
| `(seller, base-asset)`        | Seller owes the base asset    |

When a trade becomes partial or deferred, **both** keys are added to a
"deferred set". Any later trade that touches a key in this set gets
fully reversed.

### 8.2 The chain — side-by-side comparison

Using T3 and T9 from `small-sample`, both of which have Eve as a
USDT-paying buyer:

**Without strict FIFO** (what would go wrong):

```
T3   Eve buys 20 XRP from Charlie @ 200 USDT      → PARTIAL
       Charlie delivers only 10 XRP, Eve pays only 100 USDT
                            ↓
T9   Eve buys 2 ADA from Bob @ 100 USDT           → FULL ✗
       Eve still has 100 USDT in her account, trade settles
                            ↓
   Result: Bob (T9) paid in full, Charlie (T3) only got half.
           A later trade jumped the queue past an earlier one.
           Markets reject this.
```

**With strict FIFO** (what the engine does):

```
T3   Eve buys 20 XRP from Charlie @ 200 USDT      → PARTIAL
       Eve broke her USDT-paying commitment in T3
       → (Eve, USDT) added to the deferred set
                            ↓
T9   Eve buys 2 ADA from Bob @ 100 USDT           → DEFERRED ✓
       (Eve, USDT) is in the deferred set
       → T9 fully reversed even though Eve has 100 USDT
                            ↓
   Result: Both Charlie (T3) and Bob (T9) wait their turn.
           Earlier commitments honoured before later ones.
```

### 8.3 Chain of reasoning

1. **T3 fails.** Charlie can't deliver all 20 XRP to Eve — only 10 XRP move.
2. **Therefore Eve doesn't pay all 200 USDT to Charlie** — only the 100 USDT matching the 10 XRP she actually receives.
3. **Since Eve isn't paying her full USDT obligation in T3**, her USDT-paying commitment is broken for the rest of the window.
4. **So Eve won't pay USDT for T9 either**, and the engine defers T9 — even though Eve has 100 USDT sitting in her account.

> Settlement is about **commitments**, not just balances. Once a
> commitment chain breaks, every link after it has to wait too.

### 8.4 FIFO algorithm (per-iteration)

```mermaid
flowchart TD
    Start[Walk trades<br/>in execution order] --> Read[Read next trade T]
    Read --> Check{Either<br/>buyer+quoteAsset OR<br/>seller+baseAsset<br/>already in deferred set?}
    Check -- Yes --> Drag[Fully reverse T's remaining qty<br/>and mark both keys as deferred]
    Check -- No --> Partial{T is partial?<br/>BaseQty ≠ RemainingBase}
    Partial -- Yes --> Mark[Mark T's buyer+quote<br/>and seller+base as deferred<br/>so later trades will be dragged]
    Partial -- No --> Next[Continue]
    Drag --> Next
    Mark --> Next
    Next --> Read
```

---

## 9. Step 5 — Convergence Loop

After every resolution pass, FIFO knock-on, or cascade-induced new
deficit, the engine re-runs netting and re-resolves. It iterates until
no cell is negative.

```mermaid
flowchart LR
    A[netting] --> B{any negative?}
    B -- yes --> C[LIFO unwind]
    C --> D[dust check]
    D --> E[FIFO knock-on]
    E --> A
    B -- no --> F[converged ✓]
```

**On `small-sample`:** the engine converges in **5 iterations**.

| Iter | What happens                                                           |
|-----:|------------------------------------------------------------------------|
|    1 | Initial 4 deficits identified · frozen as the penalty set              |
|    2 | LIFO reversals applied (T1 full, T2 partial, T6 partial, T9 full)      |
|    3 | Cascade: Dave·ETH and Charlie·ETH go negative from T2 reversal         |
|    4 | More reversals; FIFO drags T3 → T9 via Eve·USDT                        |
|    5 | All cells non-negative → converged                                     |

The penalty set, recorded at iteration 1, never changes.

---

## 10. Step 6 — Classify Each Trade

After convergence, every trade is labelled with one of three statuses:

```mermaid
flowchart TD
    Start[Trade T after convergence] --> Q1{RemainingBase = 0<br/>OR RemainingQuote = 0<br/>OR either side<br/>below dust threshold?}
    Q1 -- Yes --> D[DEFERRED<br/>Force settled side to 0<br/>roll the whole trade forward]
    Q1 -- No --> Q2{Deferred amount<br/>= 0 OR below dust<br/>on BOTH sides?}
    Q2 -- Yes --> F[FULL<br/>Force settled side<br/>back to original size]
    Q2 -- No --> P[PARTIAL<br/>Keep current split]
```

**`small-sample` results** (`output/small-sample/trade-settlements.csv`):

| #  | Buyer / Seller   | Pair      | Status   | Settled            | Deferred           |
|----|------------------|-----------|----------|--------------------|--------------------|
| 1  | Alice / Bob      | BTC-ETH   | DEFERRED | –                  | 10 BTC ↔ 50 ETH    |
| 2  | Charlie / Dave   | BTC-ETH   | PARTIAL  | 2 BTC ↔ 10 ETH     | 3 BTC ↔ 15 ETH     |
| 3  | Eve / Charlie    | XRP-USDT  | PARTIAL  | 10 XRP ↔ 100 USDT  | 10 XRP ↔ 100 USDT  |
| 4  | Dave / Eve       | ADA-BNB   | PARTIAL  | 8 ADA ↔ 40 BNB     | 2 ADA ↔ 10 BNB     |
| 5  | Frank / Dave     | ETH-XRP   | PARTIAL  | 10 ETH ↔ 20 XRP    | 15 ETH ↔ 30 XRP    |
| 6  | Bob / Eve        | ETH-USDT  | DEFERRED | –                  | 10 ETH ↔ 200 USDT  |
| 7  | Dave / Alice     | XRP-USDT  | FULL     | 10 XRP ↔ 100 USDT  | –                  |
| 8  | Alice / Dave     | ADA-BNB   | PARTIAL  | 18 ADA ↔ 90 BNB    | 2 ADA ↔ 10 BNB     |
| 9  | Eve / Bob        | ADA-USDT  | DEFERRED | –                  | 2 ADA ↔ 100 USDT   |
| 10 | Eve / Frank      | BTC-BNB   | FULL     | 15 BTC ↔ 150 BNB   | –                  |

**Totals**: 2 FULL · 5 PARTIAL · 3 DEFERRED.

Deferred portions roll forward to the next settlement window — no value
is lost, just delayed.

---

## 11. Step 7 — Split Into Independent Batches

### 11.1 Why split

Two batches are **independent** when they share no `(member, asset)`
pair. Independent batches can be:

- Executed **in parallel** — no shared resources to lock.
- Settled **atomically per batch** — if one batch's funds movement
  fails, the others still go through.

### 11.2 How: union-find over `(member, asset)` keys

For each settled trade, the engine unions the four `(member, asset)`
keys it touches:

```
union(buyer · baseAsset,  buyer · quoteAsset)
union(buyer · baseAsset,  seller · baseAsset)
union(buyer · baseAsset,  seller · quoteAsset)
```

After all trades are processed, each connected component becomes one
batch.

### 11.3 The three batches of `small-sample`

```mermaid
flowchart LR
    subgraph B1["Batch 1 — trades 4, 8, 10  (ADA · BNB · BTC ring)"]
        direction LR
        T4(("T4"))
        T8(("T8"))
        T10(("T10"))
        DA1[Dave·ADA]
        DB1[Dave·BNB]
        EA1[Eve·ADA]
        EB1[Eve·BNB]
        AA1[Alice·ADA]
        AB1[Alice·BNB]
        ET1[Eve·BTC]
        FB1[Frank·BNB]
        FT1[Frank·BTC]
        T4 --- DA1
        T4 --- DB1
        T4 --- EA1
        T4 --- EB1
        T8 --- AA1
        T8 --- AB1
        T8 --- DA1
        T8 --- DB1
        T10 --- ET1
        T10 --- EB1
        T10 --- FB1
        T10 --- FT1
    end

    subgraph B2["Batch 2 — trade 3  (XRP · USDT)"]
        direction LR
        T3(("T3"))
        EX2[Eve·XRP]
        EU2[Eve·USDT]
        CX2[Charlie·XRP]
        CU2[Charlie·USDT]
        T3 --- EX2
        T3 --- EU2
        T3 --- CX2
        T3 --- CU2
    end

    subgraph B3["Batch 3 — trades 2, 5, 7  (BTC · ETH · XRP · USDT loop)"]
        direction LR
        T2(("T2"))
        T5(("T5"))
        T7(("T7"))
        CT3[Charlie·BTC]
        CE3[Charlie·ETH]
        DT3[Dave·BTC]
        DE3[Dave·ETH]
        FE3[Frank·ETH]
        FX3[Frank·XRP]
        DX3[Dave·XRP]
        DU3[Dave·USDT]
        AX3[Alice·XRP]
        AU3[Alice·USDT]
        T2 --- CT3
        T2 --- CE3
        T2 --- DT3
        T2 --- DE3
        T5 --- FE3
        T5 --- FX3
        T5 --- DE3
        T5 --- DX3
        T7 --- DX3
        T7 --- DU3
        T7 --- AX3
        T7 --- AU3
    end
```

Notice: **no `member · asset` node is repeated across batches.**
Dave appears in all three batches, but `Dave · ADA` only lives in
Batch 1, `Dave · BTC` only in Batch 3, etc. — they're different keys,
so the batches don't share state.

### 11.4 Deferred trades (no batch)

T1, T6, T9 contribute nothing this window. They go straight into the
next window's input.

---

## 12. Step 8 — Emit Settlement Instructions

The engine recomputes the netting matrix using the **settled**
quantities (not the original trade quantities). For every
`(member, asset)` cell where the closing balance differs from the
opening balance, it emits one instruction:

| Direction | Meaning                            |
|-----------|------------------------------------|
| `IN`      | The member's holding **increases** |
| `OUT`     | The member's holding **decreases** |

Cells that net to zero produce no instruction.

### 12.1 Worked example

Alice's ADA: opening **20**, T8 settled **18** ADA in her favour. Closing
**38**. Instruction: `Alice, ADA, +18 IN`.

### 12.2 The 22 instructions of `small-sample`

#### Batch 1 — trades 4, 8, 10 (9 instructions)

| Member | Asset | Opening |    Δ | Direction | Closing |
|--------|-------|--------:|-----:|-----------|--------:|
| Alice  | ADA   |      20 |  +18 | IN        |      38 |
| Alice  | BNB   |     200 |  −90 | OUT       |     110 |
| Eve    | ADA   |       8 |   −8 | OUT       |       0 |
| Eve    | BNB   |     200 | −110 | OUT       |      90 |
| Eve    | BTC   |      15 |  +15 | IN        |      30 |
| Dave   | ADA   |      10 |  −10 | OUT       |       0 |
| Dave   | BNB   |      50 |  +50 | IN        |     100 |
| Frank  | BNB   |     150 | +150 | IN        |     300 |
| Frank  | BTC   |      15 |  −15 | OUT       |       0 |

#### Batch 2 — trade 3 (4 instructions)

| Member  | Asset | Opening |    Δ | Direction | Closing |
|---------|-------|--------:|-----:|-----------|--------:|
| Eve     | USDT  |     100 | −100 | OUT       |       0 |
| Eve     | XRP   |      20 |  +10 | IN        |      30 |
| Charlie | USDT  |     200 | +100 | IN        |     300 |
| Charlie | XRP   |      20 |  −10 | OUT       |      10 |

#### Batch 3 — trades 2, 5, 7 (9 instructions)

| Member  | Asset | Opening |    Δ | Direction | Closing |
|---------|-------|--------:|-----:|-----------|--------:|
| Alice   | USDT  |     100 | +100 | IN        |     200 |
| Alice   | XRP   |      40 |  −10 | OUT       |      30 |
| Dave    | BTC   |       2 |   −2 | OUT       |       0 |
| Dave    | USDT  |     200 | −100 | OUT       |     100 |
| Dave    | XRP   |      12 |  +30 | IN        |      42 |
| Frank   | ETH   |       1 |  +10 | IN        |      11 |
| Frank   | XRP   |      50 |  −20 | OUT       |      30 |
| Charlie | BTC   |       5 |   +2 | IN        |       7 |
| Charlie | ETH   |      25 |  −10 | OUT       |      15 |

**Total:** 22 instructions — 11 `IN` and 11 `OUT`. They balance, because
settlement moves value, doesn't create it.

---

## 13. Numeric Precision

Every quantity is held internally as a **scaled big-integer** with
20 decimal digits of precision (i.e. the engine multiplies every input
by 10²⁰ on the way in and divides on the way out). This avoids
floating-point rounding errors and lets the engine handle any asset's
decimal places exactly.

Two per-asset values control rounding:

- **Precision** — how many decimal digits a quantity may carry. Inputs
  are rounded *down* to this precision; reversals are rounded *up*, so a
  reversal never under-undoes.
- **Dust threshold** — the smallest amount worth moving. See [§7](#7-step-3--dust-threshold-rules).

### 13.1 Dust-aware reversal flow

```mermaid
flowchart TD
    Start[Reverse qty computed<br/>from deficit] --> Snap[Round both sides<br/>up to asset precision]
    Snap --> Check{What does this leave?}
    Check -- "Settled side<br/>below dust" --> Full[Fully reverse trade<br/>crumbs not worth settling]
    Check -- "Deferred side<br/>≥ dust on BOTH sides" --> Accept[Accept current<br/>reversal amount]
    Check -- "Deferred side<br/>below dust on either side" --> Widen[Widen reversal until<br/>deferred side clears dust<br/>on the binding side]
    Widen --> Snap2[Re-snap to precision<br/>and re-check]
    Snap2 --> Check
```

**Invariant:** every leg the engine emits is either exactly zero or
strictly above its asset's dust threshold. No sub-dust residuals.

---

## 14. Why This Design

| Property                  | What the design gives us                                                                 |
|---------------------------|-------------------------------------------------------------------------------------------|
| **Liquidity efficiency**  | Netting captures intra-window inflows, so members never deliver assets they don't truly need to. |
| **Deterministic fairness**| LIFO unwinding + strict FIFO settlement produce identical output for identical input — no ordering ambiguity. |
| **Operational isolation** | Independent batches mean a hold on one group's funds doesn't freeze the others. |
| **Audit-grade arithmetic**| Scaled big-int math + explicit precision/dust rules mean the output is reproducible bit-for-bit. The validator (`cmd/validator`) re-checks the FIFO invariant and the net-flow reconciliation on every run. |
| **Fair penalty**          | Members are penalized only for shortfalls they *caused*, never for fallout the engine's cleanup introduces. |

---

## 15. Glossary

| Term                  | Meaning                                                                       |
|-----------------------|-------------------------------------------------------------------------------|
| **Base / Quote**      | The two assets in a trading pair. In `BTC-ETH`, BTC is base, ETH is quote.    |
| **Netting**           | Adding up all gains and losses per `(member, asset)` before settling.         |
| **Net position**      | A member's opening balance plus all their netted trade flows in this window.  |
| **Deficit / Shortfall**| A negative net position — a member owes more than they have.                 |
| **Initial deficit set**| The set of deficit cells frozen at the end of the **first** netting pass. The only cells the penalty logic charges against. |
| **Cascade fallout**   | New deficits introduced during convergence by reversals (not by member commitments). Never penalized. |
| **Reverse / Unwind**  | Cancelling part or all of a trade.                                            |
| **LIFO**              | Last-in-first-out — unwind the newest trade first.                            |
| **FIFO**              | First-in-first-out — earlier trades settle before later ones.                 |
| **Strict FIFO knock-on**| If trade T₁ is partial/deferred, every later trade on the same `(member, asset)` position is also fully deferred. |
| **Liability key**     | The asset side a member owes on a trade: `(buyer, quote)` or `(seller, base)`.|
| **Precision**         | Maximum decimal digits an asset's quantity may carry.                         |
| **Dust threshold**    | Smallest amount worth moving on an asset.                                     |
| **Dust zone**         | The visual region of the trade-log between zero and the dust threshold.       |
| **Batch**             | A maximal group of `(member, asset)` keys connected through settled trades.   |
| **Union-find**        | The algorithm used to compute connected components for batching.              |
| **FULL / PARTIAL / DEFERRED** | The three possible outcomes for any trade after convergence.          |
| **IN / OUT**          | Settlement-instruction direction — increase / decrease the member's holding.  |
