package corerag

import (
	"context"
	"sort"
	"strings"
	"testing"
)

// fakeTok is a deterministic TokenCounter for tests.
type fakeTok struct{}

func (fakeTok) Count(s string) int { return len(strings.Fields(s)) + 1 }

func TestAllocatePinsCoreAtRaw(t *testing.T) {
	core := Symbol{ID: "core", Name: "core", NodeType: NodeService, Signature: "func core() int", Body: "{ return 1 }"}
	nodes := []NodeCtx{
		{Symbol: core, Score: 0.9, Core: true},
	}
	priors := NewPriorTable(nil)
	sel := Allocate(nodes, 1000, priors, fakeTok{})
	if sel.Levels["core"] != LevelRaw {
		t.Fatalf("core node must be pinned at LevelRaw, got %d", sel.Levels["core"])
	}
	if sel.CoreCount != 1 {
		t.Fatalf("CoreCount = %d, want 1", sel.CoreCount)
	}
}

func TestAllocatePeripheralStartsAtStub(t *testing.T) {
	// Tiny budget so peripherals can't upgrade past stub.
	per := Symbol{ID: "per", Name: "per", NodeType: NodeService, Signature: "func per() int", Body: "{ return 1 }"}
	nodes := []NodeCtx{
		{Symbol: per, Score: 0.5, Core: false},
	}
	priors := NewPriorTable(nil)
	sel := Allocate(nodes, 2, priors, fakeTok{})
	// Stub cost = len(fields("per"))+1 = 1+1 = 2; budget=2 fits.
	if sel.Levels["per"] != LevelStub {
		t.Fatalf("peripheral should remain at stub with tiny budget, got %d", sel.Levels["per"])
	}
}

func TestAllocatePrunesLowestScoreWhenOverBudget(t *testing.T) {
	// Two peripherals whose stubs exceed budget; the lower-score one is pruned.
	p1 := Symbol{ID: "p1", Name: "p1 alpha", NodeType: NodeService, Signature: "func p1()"}
	p2 := Symbol{ID: "p2", Name: "p2 beta gamma delta", NodeType: NodeService, Signature: "func p2()"}
	nodes := []NodeCtx{
		{Symbol: p1, Score: 0.9, Core: false},
		{Symbol: p2, Score: 0.1, Core: false},
	}
	priors := NewPriorTable(nil)
	// Budget 3 fits exactly one stub: p1 stub ("p1 alpha") = 3 tokens, p2 stub
	// ("p2 beta gamma delta") = 5 tokens; combined 8 > 3, so one must go.
	sel := Allocate(nodes, 3, priors, fakeTok{})
	if _, ok := sel.Levels["p2"]; ok {
		t.Fatalf("lowest-score peripheral p2 should be pruned, but was included")
	}
	if _, ok := sel.Levels["p1"]; !ok {
		t.Fatalf("higher-score p1 should be retained")
	}
}

func TestAllocateUpgradesHighRhoFirst(t *testing.T) {
	// Two peripherals: a high-relevance Controller and a low-relevance Utility.
	// With enough budget for one upgrade, the Controller should upgrade first
	// (its R(k,1) = 0.95 vs Utility's 0.70 => higher dU).
	ctrl := Symbol{ID: "ctrl", Name: "ctrl", NodeType: NodeController, Signature: "func ctrl() int", Body: "{ return 1 }"}
	util := Symbol{ID: "util", Name: "util", NodeType: NodeUtility, Signature: "func util() int", Body: "{ return 1 }"}
	nodes := []NodeCtx{
		{Symbol: ctrl, Score: 0.9, Core: false},
		{Symbol: util, Score: 0.9, Core: false},
	}
	priors := NewPriorTable(nil)
	// Generous budget: both should be able to upgrade to raw eventually.
	sel := Allocate(nodes, 100, priors, fakeTok{})
	if sel.Levels["ctrl"] != LevelRaw {
		t.Fatalf("ctrl should upgrade to raw with ample budget, got %d", sel.Levels["ctrl"])
	}
	if sel.Levels["util"] != LevelRaw {
		t.Fatalf("util should upgrade to raw with ample budget, got %d", sel.Levels["util"])
	}
}

func TestAllocateBudgetCapsUpgrade(t *testing.T) {
	// A single peripheral whose stub fits the budget but whose next level does
	// not: the allocator must leave it at stub rather than over-spending.
	// fakeTok = fields+1, so stub(name "big") = 2 tokens; interface (signature
	// "func big() int") = 4 tokens; raw (signature + body) = 11 tokens.
	big := Symbol{ID: "big", Name: "big", NodeType: NodeService,
		Signature: "func big() int", Body: "{ return the computed result now }"}
	nodes := []NodeCtx{{Symbol: big, Score: 0.9, Core: false}}
	priors := NewPriorTable(nil)
	// Budget 2 fits the stub only (stub=2, interface=4).
	sel := Allocate(nodes, 2, priors, fakeTok{})
	if sel.Levels["big"] != LevelStub {
		t.Fatalf("should stay at stub when budget only fits stub, got %d", sel.Levels["big"])
	}
	if sel.TokensUsed > 2 {
		t.Fatalf("TokensUsed %d exceeds budget 2", sel.TokensUsed)
	}
}

func TestAllocateMixedCorePeripheral(t *testing.T) {
	core := Symbol{ID: "c", Name: "c", NodeType: NodeService, Signature: "func c() int", Body: "{ return 1 }"}
	per := Symbol{ID: "p", Name: "p", NodeType: NodeUtility, Signature: "func p() int", Body: "{ return 2 }"}
	nodes := []NodeCtx{
		{Symbol: core, Score: 0.95, Core: true},
		{Symbol: per, Score: 0.3, Core: false},
	}
	priors := NewPriorTable(nil)
	sel := Allocate(nodes, 100, priors, fakeTok{})
	if sel.CoreCount != 1 || sel.PeripheralCount != 1 {
		t.Fatalf("counts: core=%d per=%d", sel.CoreCount, sel.PeripheralCount)
	}
	// Core stays raw.
	if sel.Levels["c"] != LevelRaw {
		t.Fatalf("core not at raw: %d", sel.Levels["c"])
	}
}

func TestPriorTableFallbacks(t *testing.T) {
	p := NewPriorTable(nil)
	// Known type returns table value.
	if r := p.R(NodeUtility, LevelStub); r != 0.20 {
		t.Fatalf("R(Utility,3) = %v, want 0.20", r)
	}
	// Unknown type falls back to Other row.
	if r := p.R(NodeType("Bogus"), LevelSigDocs); r != 0.75 {
		t.Fatalf("R(Bogus,1) = %v, want Other's 0.75", r)
	}
	// k=0 always 1.0.
	if r := p.R(NodeUtility, LevelRaw); r != 1.0 {
		t.Fatalf("R(Utility,0) = %v, want 1.0", r)
	}
}

func TestPriorTableOverride(t *testing.T) {
	p := NewPriorTable(map[string]map[int]float64{
		"Utility": {3: 0.5},
	})
	if r := p.R(NodeUtility, LevelStub); r != 0.5 {
		t.Fatalf("override R(Utility,3) = %v, want 0.5", r)
	}
	// Non-overridden levels keep defaults.
	if r := p.R(NodeUtility, LevelSigDocs); r != 0.70 {
		t.Fatalf("R(Utility,1) = %v, want 0.70", r)
	}
}

func TestCoreRAGConfigValidate(t *testing.T) {
	good := DefaultCoreRAG()
	if err := good.Validate(); err != nil {
		t.Fatalf("default config should validate: %v", err)
	}
	bad := good
	bad.Budget = 0
	if err := bad.Validate(); err == nil {
		t.Fatalf("budget 0 should fail validation")
	}
	bad = good
	bad.Relevance.Theta = 1.5
	if err := bad.Validate(); err == nil {
		t.Fatalf("theta > 1 should fail validation")
	}
}

// Ensure the unused sort import is referenced (kept for clarity; remove if
// go vet complains).
var _ = sort.Ints
var _ = context.Background
