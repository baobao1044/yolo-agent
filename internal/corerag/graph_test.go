package corerag

import "testing"

func TestGraphExpandSeedsAlwaysIncluded(t *testing.T) {
	symbols := []Symbol{
		{ID: "a", OutDegree: 1},
		{ID: "b", InDegree: 1},
		{ID: "c"},
	}
	edges := []Edge{
		{From: "a", To: "b", Kind: EdgeCall, Weight: 1.0},
		{From: "b", To: "c", Kind: EdgeCall, Weight: 1.0},
	}
	g := NewGraph(symbols, edges)

	// Even with theta=1.0 (all candidates pruned), seeds must be present.
	got := g.Expand([]string{"a"}, 2, 1.0, func(id string) float64 { return 0 })
	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("seed must always be included, got %v", got)
	}
}

func TestGraphExpandBFSWithTheta(t *testing.T) {
	symbols := []Symbol{
		{ID: "a", OutDegree: 1},
		{ID: "b", InDegree: 1, OutDegree: 1},
		{ID: "c", InDegree: 1},
	}
	edges := []Edge{
		{From: "a", To: "b", Kind: EdgeCall, Weight: 1.0},
		{From: "b", To: "c", Kind: EdgeCall, Weight: 1.0},
	}
	g := NewGraph(symbols, edges)

	// scoreFn: b passes theta, c does not. With depth 2, expansion includes
	// a (seed) and b (S>=theta), but not c.
	score := map[string]float64{"a": 0.9, "b": 0.5, "c": 0.05}
	got := g.Expand([]string{"a"}, 2, 0.15, func(id string) float64 { return score[id] })
	want := map[string]bool{"a": true, "b": true, "c": false}
	for _, id := range got {
		if id == "c" {
			t.Fatalf("c should be pruned (S<theta), but was included")
		}
		if !want[id] {
			t.Fatalf("unexpected node %q included", id)
		}
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 nodes (a,b), got %d: %v", len(got), got)
	}
}

func TestGraphExpandDepthLimit(t *testing.T) {
	symbols := []Symbol{
		{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"},
	}
	edges := []Edge{
		{From: "a", To: "b", Kind: EdgeCall},
		{From: "b", To: "c", Kind: EdgeCall},
		{From: "c", To: "d", Kind: EdgeCall},
	}
	g := NewGraph(symbols, edges)
	// depth 1: only a and its direct neighbor b.
	got := g.Expand([]string{"a"}, 1, 0.0, func(string) float64 { return 1.0 })
	if len(got) != 2 {
		t.Fatalf("depth 1 should yield 2 nodes, got %d: %v", len(got), got)
	}
}

func TestGraphMaxOutDegree(t *testing.T) {
	edges := []Edge{
		{From: "a", To: "b"}, {From: "a", To: "c"}, {From: "b", To: "c"},
	}
	g := NewGraph([]Symbol{{ID: "a"}, {ID: "b"}, {ID: "c"}}, edges)
	if g.MaxOutDegree() != 2 {
		t.Fatalf("MaxOutDegree = %d, want 2", g.MaxOutDegree())
	}
}

func TestCosine(t *testing.T) {
	if v := cosine([]float32{1, 0}, []float32{1, 0}); v != 1 {
		t.Fatalf("identical vectors: %v", v)
	}
	if v := cosine([]float32{1, 0}, []float32{0, 1}); v != 0 {
		t.Fatalf("orthogonal vectors: %v", v)
	}
	if v := cosine([]float32{}, []float32{}); v != 0 {
		t.Fatalf("empty vectors: %v", v)
	}
}
