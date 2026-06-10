# How the Settlement Engine Works — A Business Walk-Through

This page explains, in plain English, what the multilateral settlement engine
does. We use the `small-sample` dataset to illustrate each step. No code, no
maths beyond simple arithmetic.

---

## 1. The Problem We're Solving

At the end of a trading window we have:

- A list of **trades** that members agreed to with each other.
- A list of **member ledger balances** — how much of each asset each member
  actually holds *right now*.
- A list of **assets** with their precision (decimal places) and a tiny
  "dust" threshold below which amounts are considered uneconomical to move.

Some members may have agreed to deliver more of an asset than they actually
own. We cannot magically conjure assets, so some trades will have to **wait
(defer)** or **partially settle**.

The engine's job is to decide:

1. Which trades settle in full, which settle partially, and which are
   pushed to the next window.
2. The minimum set of debits and credits — the **settlement
   instructions** — that need to move on the books.
3. How to bundle independent groups of members into **batches** that can be
   settled atomically and independently of each other.

---

## 2. The `small-sample` Dataset

### Members and opening balances (`ledger.csv`)

| Member  | BTC | ETH | XRP | ADA | BNB | USDT |
|---------|----:|----:|----:|----:|----:|-----:|
| Alice   |   8 |  50 |  40 |  20 | 200 |  100 |
| Bob     |   0 |  55 |   – |  50 |   – |    0 |
| Charlie |   5 |  25 |  20 |   – |   – |  200 |
| Dave    |   2 |   – |  12 |  10 |  50 |  200 |
| Eve     |  15 |   5 |  20 |   8 | 200 |  100 |
| Frank   |  15 |   1 |  50 |   0 | 150 |    0 |

### Trades (`trades.csv`)

| #  | Buyer   | Seller  | Buys   | Pays     |
|----|---------|---------|--------|----------|
| 1  | Alice   | Bob     | 10 BTC | 50 ETH   |
| 2  | Charlie | Dave    | 5 BTC  | 25 ETH   |
| 3  | Eve     | Charlie | 20 XRP | 200 USDT |
| 4  | Dave    | Eve     | 10 ADA | 50 BNB   |
| 5  | Frank   | Dave    | 25 ETH | 50 XRP   |
| 6  | Bob     | Eve     | 10 ETH | 200 USDT |
| 7  | Dave    | Alice   | 10 XRP | 100 USDT |
| 8  | Alice   | Dave    | 20 ADA | 100 BNB  |
| 9  | Eve     | Bob     | 2 ADA  | 100 USDT |
| 10 | Eve     | Frank   | 15 BTC | 150 BNB  |

Trades are listed in the order they executed — order matters later.

---

## 3. Step 1 — Net Everything

We pretend, for a moment, that every trade settles in full. We add up what
each member would gain or lose per asset.

For each trade:
- Buyer's **base** balance goes up; buyer's **quote** balance goes down.
- Seller's **base** balance goes down; seller's **quote** balance goes up.

Applied to the small sample, several members end up **negative** (cannot
afford the deal):

| Member | Asset | Opening | After all trades | Shortfall |
|--------|-------|--------:|-----------------:|----------:|
| Bob    | BTC   |       0 |              −10 | 10 BTC    |
| Bob    | USDT  |       0 |             −100 | 100 USDT  |
| Dave   | BTC   |       2 |               −3 | 3 BTC     |
| Eve    | ETH   |       5 |               −5 | 5 ETH     |

Everyone else is non-negative — they can cover their side.

> **Why net first?** A member may be short of an asset on one trade but
> *receive* that same asset on another trade. Netting captures that. We
> only need to act on what is *truly* uncovered after all incoming flows
> are considered.

---

## 4. Step 2 — Resolve Shortfalls by Reversing the Newest Trade First

For every shortfall, the engine **partially or fully reverses** the
member's most recent trade that involves the shorted asset. If one
reversal isn't enough, it walks backward to the next-newest trade, and so
on, until the shortfall is gone.

> **Why newest first?** "Last in, first out" preserves the earlier trades,
> which a market typically expects to honour ahead of the later ones.

Reversing a trade is just unwinding it: the buyer no longer gets the base,
the seller keeps it; the quote stays with the buyer. Crucially, when we
unwind part of one side, the **other side is unwound in the same
proportion** so the price ratio of the trade is preserved.

### What happens in `small-sample`

- **Bob is short 10 BTC** (owed in T1). The newest BTC obligation
  Bob has is T1 itself. T1 is fully reversed — Bob never
  delivers BTC, Alice never delivers the 50 ETH. **T1 → DEFERRED.**

- **Bob is short 100 USDT** (T6 + T9 obligations). Newest first: T9
  fully reverses (only 100 USDT involved). That clears the shortfall, but
  later FIFO logic will deal with T6 too.

- **Dave is short 3 BTC** (out of the 5 owed in T2). T2 is partially
  reversed by 3 BTC. Dave now delivers only **2 BTC** instead of 5, and
  Charlie pays only **10 ETH** instead of 25 (the price ratio holds).
  **T2 → PARTIAL.**

- **Eve is short 5 ETH** (owed in T6). T6 is the newest ETH
  obligation. Reversing 5 ETH of T6 would leave a weird half-trade, so
  the engine — combined with the FIFO rule below — fully reverses T6.
  **T6 → DEFERRED.**

### Dust handling

After a partial reversal, we check both sides:

- If what's **left settled** is below an asset's dust threshold, the
  whole trade is deferred (settling crumbs isn't worth the move).
- If what's **deferred** is below dust on either side, we widen the
  reversal until both sides are cleanly above dust.

In the small sample, all asset units are large round numbers, so dust
doesn't bite — but on real data it prevents the engine from emitting
0.000001-BTC dribbles.

---

## 5. Step 3 — Strict FIFO Knock-On (when enabled)

Markets typically demand that once a member's earlier trade is
**not honoured in full**, every *later* trade on the same side of the
same asset is also deferred. We don't let trade #9 settle if trade #3 for
the same member-asset position was only partial.

In our sample, T6 (Bob buying ETH, paying USDT) ends up deferred.
**T9** is a later trade where Bob is on the USDT-owing side again.
FIFO therefore drags T9 into deferral. **T9 → DEFERRED.**

T3 (Eve buys XRP, pays USDT) and T6 share Eve's USDT-receive
side, but T6 *follows* T3, so T3 is not affected.

After this pass, the engine re-runs Step 2 to clean up any new
imbalances. It loops until there is nothing negative left.

---

## 6. Step 4 — Classify Each Trade

When the dust settles (literally), every trade gets one of three labels:

| Status     | What it means                                        |
|------------|------------------------------------------------------|
| `FULL`     | The full quantity settled. No leftover.              |
| `PARTIAL`  | A portion settled; the rest is deferred to next run. |
| `DEFERRED` | None of it settled this run.                         |

For `small-sample`:

| #  | Buyer / Seller   | Status   | Settled           | Deferred          |
|----|------------------|----------|-------------------|-------------------|
| 1  | Alice / Bob      | DEFERRED | –                 | 10 BTC ↔ 50 ETH   |
| 2  | Charlie / Dave   | PARTIAL  | 2 BTC ↔ 10 ETH    | 3 BTC ↔ 15 ETH    |
| 3  | Eve / Charlie    | PARTIAL  | 10 XRP ↔ 100 USDT | 10 XRP ↔ 100 USDT |
| 4  | Dave / Eve       | PARTIAL  | 8 ADA ↔ 40 BNB    | 2 ADA ↔ 10 BNB    |
| 5  | Frank / Dave     | PARTIAL  | 10 ETH ↔ 20 XRP   | 15 ETH ↔ 30 XRP   |
| 6  | Bob / Eve        | DEFERRED | –                 | 10 ETH ↔ 200 USDT |
| 7  | Dave / Alice     | FULL     | 10 XRP ↔ 100 USDT | –                 |
| 8  | Alice / Dave     | PARTIAL  | 18 ADA ↔ 90 BNB   | 2 ADA ↔ 10 BNB    |
| 9  | Eve / Bob        | DEFERRED | –                 | 2 ADA ↔ 100 USDT  |
| 10 | Eve / Frank      | FULL     | 15 BTC ↔ 150 BNB  | –                 |

The deferred portions roll forward to the next settlement window.

---

## 7. Step 5 — Generate Settlement Instructions

The engine now looks at each member-asset pair and emits one instruction
per net movement: `IN` (asset increases) or `OUT` (asset decreases). No
instruction is emitted where the net change is zero.

**Reading example:** Alice ends the window with 38 ADA. She started with
20 ADA. The instruction is `Alice, ADA, +18 IN`.

---

## 8. Step 6 — Split Into Independent Batches

A "batch" is a group of member-asset positions that touch the same
trades. Two batches that share no member-asset pair can be settled
**independently and atomically** — one batch failing does not block the
others.

In `small-sample`, the engine produces three batches.

### Batch 1 — ADA / BNB / BTC ring around Dave, Eve, Frank, Alice

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

This batch covers trades 4, 8, and 10.

### Batch 2 — XRP / USDT between Eve and Charlie

| Member  | Asset | Opening |    Δ | Direction | Closing |
|---------|-------|--------:|-----:|-----------|--------:|
| Eve     | USDT  |     100 | −100 | OUT       |       0 |
| Eve     | XRP   |      20 |  +10 | IN        |      30 |
| Charlie | USDT  |     200 | +100 | IN        |     300 |
| Charlie | XRP   |      20 |  −10 | OUT       |      10 |

This batch covers the partial of trade 3.

### Batch 3 — BTC / ETH / XRP / USDT loop around Alice, Dave, Frank, Charlie

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

This batch covers trades 2, 5, and 7.

### Deferred trades (no batch)

Trades 1, 6, and 9 settle nothing this window. They go back into the
queue for the next run.

---

## 9. Why This Matters

- **Liquidity efficiency.** We never ask a member to deliver an asset
  they don't have, but we also never under-settle when they *do* have it.
- **Fairness via FIFO.** Earlier trades get priority over later ones if
  someone is short.
- **Operational isolation.** Independent batches mean a glitch in one
  group's transfer doesn't block the others.
- **No dust dust-ups.** Tiny residual amounts never appear on a
  settlement leg; the engine always rounds them out of existence in a
  way that preserves the trade's price.

---

## 10. Quick Glossary

| Term                  | Meaning                                                              |
|-----------------------|----------------------------------------------------------------------|
| Base / Quote          | The two assets in a trading pair (e.g. BTC-ETH: BTC is base, ETH quote). |
| Netting               | Adding up all gains and losses per member-asset before settling.    |
| Deficit / Shortfall   | A negative net position — a member owes more than they have.         |
| Reverse / Unwind      | Cancelling part or all of a trade.                                   |
| Dust                  | An amount so small it isn't worth moving.                            |
| FIFO                  | First-in-first-out: earlier trades take priority over later ones.    |
| Batch                 | An independent group of settlement instructions that move together.  |
| FULL / PARTIAL / DEFERRED | The three possible outcomes for any trade.                       |
