package corerag

import (
	"sort"
)

// NodeCtx is a candidate node for allocation, carrying everything the greedy
// allocator needs: the symbol, its relevance score S(v), and whether it is a
// core node (V_0, pinned to full source) or peripheral (V_p, upgradeable).
type NodeCtx struct {
	Symbol Symbol
	Score  float64 // S(v) in [0,1]
	Core   bool    // true => V_0 (pinned at LevelRaw); false => V_p (upgradeable)
}

// Selection is the result of the greedy allocation: per-node chosen level and
// aggregate statistics.
type Selection struct {
	Levels    map[string]Level // symbol ID -> chosen compression level
	TokensUsed int             // estimated tokens consumed by the selection
	Nodes      int             // number of nodes included
	CoreCount  int             // number of core nodes (pinned k=0)
	PeripheralCount int       // number of peripheral nodes
}

// Allocate runs the Budget-Aware Marginal Utility Greedy Allocation (CORE doc
// Algorithm 1):
//
//  1. Core nodes V_0 are pinned at LevelRaw (full source); their cost is
//     deducted from the budget first.
//  2. Peripheral nodes V_p start at LevelStub (cheapest). If even stubs exceed
//     the budget, the lowest-S(v) nodes are pruned until it fits.
//  3. A transition pool P of upgrades (k -> k-1) is built, each with marginal
//     utility dU = S(v)*R(k-1,Type) - S(v)*R(k,Type) and marginal cost
//     dc = cost(k-1) - cost(k), and efficiency rho = dU/dc.
//  4. Upgrades are applied in decreasing rho order while the budget allows.
//
// Complexity is O(|V| log |V|), dominated by sorting P (CORE doc §4.3).
func Allocate(nodes []NodeCtx, budget int, priors *PriorTable, tok interface{ Count(string) int }) Selection {
	sel := Selection{Levels: make(map[string]Level, len(nodes))}

	// Phase 1: pin core nodes at raw.
	used := 0
	var peripheral []NodeCtx
	for _, n := range nodes {
		if n.Core {
			c := tok.Count(Materialize(n.Symbol, LevelRaw))
			used += c
			sel.Levels[n.Symbol.ID] = LevelRaw
			sel.Nodes++
			sel.CoreCount++
		} else {
			peripheral = append(peripheral, n)
		}
	}

	if budget <= 0 {
		return sel
	}

	// Phase 2: start peripheral at stub; prune lowest-score if over budget.
	sortPeripheralByScore(peripheral) // ascending score so we prune from the end
	for _, n := range peripheral {
		sel.Levels[n.Symbol.ID] = LevelStub
		used += tok.Count(Materialize(n.Symbol, LevelStub))
		sel.Nodes++
		sel.PeripheralCount++
	}
	for used > budget && len(peripheral) > 0 {
		// Prune the lowest-S(v) peripheral node.
		victim := peripheral[len(peripheral)-1]
		peripheral = peripheral[:len(peripheral)-1]
		used -= tok.Count(Materialize(victim.Symbol, sel.Levels[victim.Symbol.ID]))
		delete(sel.Levels, victim.Symbol.ID)
		sel.Nodes--
		sel.PeripheralCount--
	}

	// Phase 3: build the transition pool of upgrades.
	type upgrade struct {
		nodeIdx int
		from    Level
		to      Level // strictly from-1
		dU      float64
		dC      int
		rho     float64
	}
	var pool []upgrade
	for i, n := range peripheral {
		cur := sel.Levels[n.Symbol.ID]
		for cur > LevelRaw {
			to := cur - 1
			dU := n.Score*priors.R(n.Symbol.NodeType, to) - n.Score*priors.R(n.Symbol.NodeType, cur)
			dC := tok.Count(Materialize(n.Symbol, to)) - tok.Count(Materialize(n.Symbol, cur))
			if dC <= 0 {
				// A "cheaper" upgrade: take it immediately (free improvement).
				used += dC
				sel.Levels[n.Symbol.ID] = to
				cur = to
				continue
			}
			pool = append(pool, upgrade{i, cur, to, dU, dC, dU / float64(dC)})
			break // consider the next node's first worthwhile upgrade
		}
	}

	// Phase 4: apply upgrades in decreasing rho order.
	sort.Slice(pool, func(i, j int) bool { return pool[i].rho > pool[j].rho })
	for len(pool) > 0 {
		// Pop the highest-rho upgrade.
		best := pool[0]
		pool = pool[1:]
		node := peripheral[best.nodeIdx]
		// Re-validate: the node's current level may have advanced already (via
		// free upgrades queued by earlier iterations). Re-derive the next
		// worthwhile upgrade from its current level, looping past any free
		// upgrades so the node is never left mid-way when it could advance
		// further at no cost.
		if sel.Levels[node.Symbol.ID] != best.from {
			cur := sel.Levels[node.Symbol.ID]
			for cur > LevelRaw {
				to := cur - 1
				dC := tok.Count(Materialize(node.Symbol, to)) - tok.Count(Materialize(node.Symbol, cur))
				if dC <= 0 {
					used += dC
					sel.Levels[node.Symbol.ID] = to
					cur = to
					continue
				}
				if used+dC <= budget {
					dU := node.Score*priors.R(node.Symbol.NodeType, to) - node.Score*priors.R(node.Symbol.NodeType, cur)
					pool = append(pool, upgrade{best.nodeIdx, cur, to, dU, dC, dU / float64(dC)})
					sort.Slice(pool, func(i, j int) bool { return pool[i].rho > pool[j].rho })
				}
				break
			}
			continue
		}
		if used+best.dC <= budget {
			used += best.dC
			sel.Levels[node.Symbol.ID] = best.to
			// Offer the next upgrade for this node, looping past free upgrades
			// (dC<=0) which are applied immediately, and queuing the first paid
			// upgrade (dC>0) back into the pool in rho order.
			cur := best.to
			for cur > LevelRaw {
				to := cur - 1
				dU := node.Score*priors.R(node.Symbol.NodeType, to) - node.Score*priors.R(node.Symbol.NodeType, cur)
				dC := tok.Count(Materialize(node.Symbol, to)) - tok.Count(Materialize(node.Symbol, cur))
				if dC <= 0 {
					used += dC
					sel.Levels[node.Symbol.ID] = to
					cur = to
					continue
				}
				pool = append(pool, upgrade{best.nodeIdx, cur, to, dU, dC, dU / float64(dC)})
				sort.Slice(pool, func(i, j int) bool { return pool[i].rho > pool[j].rho })
				break
			}
		}
	}

	sel.TokensUsed = used
	return sel
}

// sortPeripheralByScore sorts peripheral nodes descending by S(v) so the
// lowest relevance is at the end (pruning target): when stubs exceed the
// budget we drop nodes from the end, i.e. lowest-S(v) first.
func sortPeripheralByScore(p []NodeCtx) {
	sort.SliceStable(p, func(i, j int) bool { return p[i].Score > p[j].Score })
}
