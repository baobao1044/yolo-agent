package corerag

import (
	"math"
)

// Graph is the in-memory call/import structure graph for one repo. It supports
// BFS expansion from seed symbols, dropping nodes whose relevance falls below
// the threshold theta (CORE doc §4.1: "only expand to vertices satisfying
// S(v) >= theta, controlling Context Explosion at the structural level").
type Graph struct {
	symbols   map[string]Symbol    // id -> symbol
	adjacency map[string][]Edge    // from -> edges
	reverse   map[string][]Edge    // to -> edges (for in-degree lookups)
}

// NewGraph builds a graph from symbols and edges.
func NewGraph(symbols []Symbol, edges []Edge) *Graph {
	g := &Graph{
		symbols:   make(map[string]Symbol, len(symbols)),
		adjacency: make(map[string][]Edge),
		reverse:   make(map[string][]Edge),
	}
	for _, s := range symbols {
		g.symbols[s.ID] = s
	}
	for _, e := range edges {
		if e.From == "" || e.To == "" {
			continue
		}
		g.adjacency[e.From] = append(g.adjacency[e.From], e)
		g.reverse[e.To] = append(g.reverse[e.To], e)
	}
	return g
}

// Symbol returns the symbol with the given ID, if present.
func (g *Graph) Symbol(id string) (Symbol, bool) {
	s, ok := g.symbols[id]
	return s, ok
}

// Symbols returns all symbols in the graph.
func (g *Graph) Symbols() []Symbol {
	out := make([]Symbol, 0, len(g.symbols))
	for _, s := range g.symbols {
		out = append(out, s)
	}
	return out
}

// Neighbors returns the edges leaving the given symbol.
func (g *Graph) Neighbors(id string) []Edge {
	return g.adjacency[id]
}

// MaxOutDegree returns the largest out-degree in the graph, for centrality
// normalization.
func (g *Graph) MaxOutDegree() int {
	max := 0
	for _, es := range g.adjacency {
		if len(es) > max {
			max = len(es)
		}
	}
	return max
}

// Expand performs BFS from the seeds up to the given depth, collecting the set
// of reachable symbol IDs (including seeds). The scoreFn assigns a relevance
// S(v) in [0,1] to each candidate; only nodes with S(v) >= theta are kept and
// traversed further, bounding Context Explosion (CORE doc §4.1).
//
// The returned set preserves insertion order (seeds first, then BFS order).
func (g *Graph) Expand(seeds []string, depth int, theta float64, scoreFn func(string) float64) []string {
	if len(seeds) == 0 {
		return nil
	}
	visited := make(map[string]bool, len(seeds)*4)
	type item struct {
		id    string
		depth int
	}
	queue := make([]item, 0, len(seeds))
	var out []string

	// Seeds are always included regardless of score (they are V_0).
	for _, s := range seeds {
		if g.symbols[s].ID == "" && !visited[s] {
			// Seed may still be a synthetic import target; include if present.
		}
		if !visited[s] {
			visited[s] = true
			out = append(out, s)
			queue = append(queue, item{s, 0})
		}
	}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.depth >= depth {
			continue
		}
		for _, e := range g.adjacency[cur.id] {
			if visited[e.To] {
				continue
			}
			if scoreFn != nil && scoreFn(e.To) < theta {
				continue
			}
			visited[e.To] = true
			out = append(out, e.To)
			queue = append(queue, item{e.To, cur.depth + 1})
		}
	}
	return out
}

// cosine returns the cosine similarity between two vectors. Zero-length or
// mismatched-length vectors yield 0.
func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		ai := float64(a[i])
		bi := float64(b[i])
		dot += ai * bi
		na += ai * ai
		nb += bi * bi
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
