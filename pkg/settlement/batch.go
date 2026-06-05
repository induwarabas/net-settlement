package settlement

import (
	"fmt"
	"sort"
)

//type unionFind struct {
//	parent map[string]string
//	rank   map[string]int
//}
//
//func newUnionFind() *unionFind {
//	return &unionFind{
//		parent: make(map[string]string),
//		rank:   make(map[string]int),
//	}
//}
//
//// ensure adds x to the union-find structure if it is not already present.
//func (uf *unionFind) ensure(x string) {
//	if _, ok := uf.parent[x]; !ok {
//		uf.parent[x] = x
//		uf.rank[x] = 0
//	}
//}
//
//// find returns the root representative of x's component, applying path compression.
//func (uf *unionFind) find(x string) string {
//	uf.ensure(x)
//	if uf.parent[x] != x {
//		uf.parent[x] = uf.find(uf.parent[x])
//	}
//	return uf.parent[x]
//}
//
//// union merges the components containing a and b using union-by-rank.
//func (uf *unionFind) union(a, b string) {
//	ra, rb := uf.find(a), uf.find(b)
//	if ra == rb {
//		return
//	}
//	if uf.rank[ra] < uf.rank[rb] {
//		ra, rb = rb, ra
//	}
//	uf.parent[rb] = ra
//	if uf.rank[ra] == uf.rank[rb] {
//		uf.rank[ra]++
//	}
//}

// batchBuilder is a flat-parent union-find over string keys used to group
// member-asset cells into independent settlement batches. Each element's
// parent is updated eagerly on union so root lookups are O(1); the trade-off
// is that union is O(size of the smaller component) rather than near-constant.
type batchBuilder struct {
	parents  map[string]string
	children map[string][]string
}

// newBatchBuilder returns an empty batchBuilder.
func newBatchBuilder() *batchBuilder {
	return &batchBuilder{
		parents:  make(map[string]string),
		children: make(map[string][]string),
	}
}

// ensure registers x as its own component if not already present.
func (m *batchBuilder) ensure(x string) {
	if _, ok := m.parents[x]; !ok {
		m.parents[x] = x
		m.children[x] = make([]string, 1)
		m.children[x][0] = x
	}
}

// union merges the components containing a and b. After it returns, every
// element of b's old component reports a's root.
func (m *batchBuilder) union(a, b string) {
	ra, rb := m.parents[a], m.parents[b]
	if ra == rb {
		return
	}

	for _, child := range m.children[rb] {
		m.children[ra] = append(m.children[ra], child)
		m.parents[child] = ra
		delete(m.children, rb)
	}
}

// root returns the component representative for x. x must have been registered
// via ensure (directly or as part of a union).
func (m *batchBuilder) root(x string) string {
	return m.parents[x]
}

// memberAssetKey returns the canonical string key for a (member, asset) pair.
func memberAssetKey(member, asset string) string {
	if member == "M02_T1" && asset == "EUR" {
		fmt.Println("here")
	}
	return member + "-" + asset
}

// splitBatches partitions a single settlement result into independent batches
// using a union-find over member-asset keys. Trades and instructions that share
// no member-asset overlap end up in separate batches and can be settled
// atomically and independently. Deferred trades are collected separately and
// returned in Results.Deferred.
func splitBatches(result *Result) Results {
	if result == nil || (len(result.Trades) == 0 && len(result.Instructions) == 0) {
		return Results{}
	}

	var deferred []Trade
	var settled []*TradeResult
	for _, tr := range result.Trades {
		if tr.Status == TradeResultStatusDeferred {
			deferred = append(deferred, tr.Trade)
		} else {
			settled = append(settled, tr)
		}
	}

	uf := newBatchBuilder()

	for _, tr := range settled {
		t := tr.Trade
		buyerBase := memberAssetKey(t.Buyer(), t.BaseAsset())
		buyerQuote := memberAssetKey(t.Buyer(), t.QuoteAsset())
		sellerBase := memberAssetKey(t.Seller(), t.BaseAsset())
		sellerQuote := memberAssetKey(t.Seller(), t.QuoteAsset())

		if buyerBase == "M02_T1-EUR" || buyerQuote == "M02_T1-EUR" || sellerBase == "M02_T1-EUR" || sellerQuote == "M02_T1-EUR" {
			fmt.Println("here")
		}

		uf.ensure(buyerBase)
		uf.ensure(buyerQuote)
		uf.ensure(sellerBase)
		uf.ensure(sellerQuote)
		uf.union(buyerBase, buyerQuote)
		uf.union(buyerBase, sellerBase)
		uf.union(buyerBase, sellerQuote)
	}

	for _, inst := range result.Instructions {
		uf.ensure(memberAssetKey(inst.Member, inst.Asset))
	}

	tradesByRoot := make(map[string][]*TradeResult)
	for _, tr := range settled {
		root := uf.root(memberAssetKey(tr.Trade.Buyer(), tr.Trade.BaseAsset()))
		tradesByRoot[root] = append(tradesByRoot[root], tr)
	}

	instrByRoot := make(map[string][]*Instruction)
	for _, inst := range result.Instructions {
		root := uf.root(memberAssetKey(inst.Member, inst.Asset))
		instrByRoot[root] = append(instrByRoot[root], inst)
	}

	roots := make(map[string]struct{})
	for r := range tradesByRoot {
		roots[r] = struct{}{}
	}
	for r := range instrByRoot {
		roots[r] = struct{}{}
	}

	sorted := make([]string, 0, len(roots))
	for r := range roots {
		sorted = append(sorted, r)
	}
	sort.Strings(sorted)

	batches := make([]*Result, 0, len(sorted))
	for _, r := range sorted {
		batches = append(batches, &Result{
			Trades:       tradesByRoot[r],
			Instructions: instrByRoot[r],
		})
	}
	return Results{Deferred: deferred, Batches: batches}
}
