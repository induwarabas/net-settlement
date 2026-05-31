package settlement

import "sort"

type unionFind struct {
	parent map[string]string
	rank   map[string]int
}

func newUnionFind() *unionFind {
	return &unionFind{
		parent: make(map[string]string),
		rank:   make(map[string]int),
	}
}

func (uf *unionFind) ensure(x string) {
	if _, ok := uf.parent[x]; !ok {
		uf.parent[x] = x
		uf.rank[x] = 0
	}
}

func (uf *unionFind) find(x string) string {
	uf.ensure(x)
	if uf.parent[x] != x {
		uf.parent[x] = uf.find(uf.parent[x])
	}
	return uf.parent[x]
}

func (uf *unionFind) union(a, b string) {
	ra, rb := uf.find(a), uf.find(b)
	if ra == rb {
		return
	}
	if uf.rank[ra] < uf.rank[rb] {
		ra, rb = rb, ra
	}
	uf.parent[rb] = ra
	if uf.rank[ra] == uf.rank[rb] {
		uf.rank[ra]++
	}
}

func memberAssetKey(member, asset string) string {
	return member + "\x00" + asset
}

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

	uf := newUnionFind()

	for _, tr := range settled {
		t := tr.Trade
		buyerBase := memberAssetKey(t.Buyer(), t.BaseAsset())
		buyerQuote := memberAssetKey(t.Buyer(), t.QuoteAsset())
		sellerBase := memberAssetKey(t.Seller(), t.BaseAsset())
		sellerQuote := memberAssetKey(t.Seller(), t.QuoteAsset())
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
		root := uf.find(memberAssetKey(tr.Trade.Buyer(), tr.Trade.BaseAsset()))
		tradesByRoot[root] = append(tradesByRoot[root], tr)
	}

	instrByRoot := make(map[string][]*Instruction)
	for _, inst := range result.Instructions {
		root := uf.find(memberAssetKey(inst.Member, inst.Asset))
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
