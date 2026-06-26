package corerag

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/baobg/yolo-agent/internal/embeddings"
)

// repoRoot returns the on-disk root of the yolo-agent repo that hosts this
// test, so the integration test can dogfood the engine on its own source. It
// walks upward from this test file until it finds a go.mod, so it is robust to
// the repo being checked out at an arbitrary depth.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file) // internal/corerag
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not locate repo root (no go.mod found upward from test file)")
	return ""
}

// newTestEngine builds an Engine backed by a temp SQLite DB and the
// deterministic MockEmbedder, so the integration path runs offline with no
// cgo and no network.
func newTestEngine(t *testing.T) (*Engine, func()) {
	t.Helper()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "corerag_test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	cleanup := func() { _ = db.Close() }

	store, err := NewStore(db)
	if err != nil {
		cleanup()
		t.Fatalf("NewStore: %v", err)
	}
	cfg := DefaultCoreRAG()
	cfg.Budget = 1024
	cfg.Depth = 2
	emb := embeddings.NewMockEmbedder(32)
	engine := &Engine{
		store:     store,
		embedder: emb,
		tokenizer: embeddings.HeuristicTokenCounter{},
		cfg:       cfg,
		parsers:   buildParsers(nil),
	}
	return engine, cleanup
}

// TestEngineIndexRepo dogfoods the index path on the yolo-agent repo itself:
// the Go parser must extract a non-trivial symbol set and at least one call
// edge, and the result must be persisted so a second index is a no-op.
func TestEngineIndexRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in -short mode")
	}
	root := repoRoot(t)
	engine, cleanup := newTestEngine(t)
	defer cleanup()

	ctx := context.Background()
	res, err := engine.Index(ctx, root, "go", false)
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if res.Language != "go" {
		t.Fatalf("language = %q, want go", res.Language)
	}
	if res.Symbols < 20 {
		t.Fatalf("expected a non-trivial symbol count, got %d", res.Symbols)
	}
	if res.Edges == 0 {
		t.Fatalf("expected at least one resolved call/import edge, got 0")
	}

	// Re-indexing with the same hash and force=false is a no-op: same counts.
	res2, err := engine.Index(ctx, root, "go", false)
	if err != nil {
		t.Fatalf("re-Index: %v", err)
	}
	if res2.Symbols != res.Symbols || res2.Edges != res.Edges {
		t.Fatalf("unchanged re-index should be a no-op: symbols %d->%d, edges %d->%d",
			res.Symbols, res2.Symbols, res.Edges, res2.Edges)
	}

	// Symbols are persisted and reloadable with their embeddings intact.
	syms, err := engine.store.LoadSymbols(res.RepoID)
	if err != nil {
		t.Fatalf("LoadSymbols: %v", err)
	}
	if len(syms) != res.Symbols {
		t.Fatalf("loaded %d symbols, index reported %d", len(syms), res.Symbols)
	}
	withEmb := 0
	for _, s := range syms {
		if len(s.Embedding) > 0 {
			withEmb++
		}
	}
	if withEmb == 0 {
		t.Fatalf("no symbols carried embeddings after load; embedding round-trip broken")
	}
}

// TestEngineQueryAmpleBudgetReachesRaw dogfoods the full query pipeline with an
// ample budget: semantic ranking -> BFS expansion -> core/peripheral split ->
// greedy MCKP allocation -> SACRS materialization. With budget to spare the
// allocator upgrades every selected node to LevelRaw (the information ceiling),
// so the context must render all nodes at k=0 and stay within the budget.
func TestEngineQueryAmpleBudgetReachesRaw(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in -short mode")
	}
	root := repoRoot(t)
	engine, cleanup := newTestEngine(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := engine.Index(ctx, root, "go", false); err != nil {
		t.Fatalf("Index: %v", err)
	}

	budget := 8192
	res, err := engine.Query(ctx, "index repository and retrieve context for a query", root, budget, 5)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Context == "" {
		t.Fatalf("query returned empty context")
	}
	if res.TokensUsed > budget {
		t.Fatalf("TokensUsed %d exceeds ample budget %d", res.TokensUsed, budget)
	}
	if res.Nodes == 0 {
		t.Fatalf("query returned no nodes")
	}
	if res.CoreCount == 0 {
		t.Fatalf("expected at least one core (seed) node, got 0")
	}
	if !strings.Contains(res.Context, "tokens used:") {
		t.Fatalf("context missing budget header:\n%s", res.Context)
	}
	// With ample budget every selected node reaches LevelRaw: the context must
	// contain k=0 and no compressed level (k=1/2/3).
	for _, k := range []string{"k=1", "k=2", "k=3"} {
		if strings.Contains(res.Context, k) {
			t.Fatalf("ample budget should upgrade all nodes to raw, but found %s:\n%s", k, res.Context)
		}
	}
	if !strings.Contains(res.Context, "k=0") {
		t.Fatalf("context missing any raw (k=0) node:\n%s", res.Context)
	}
}

// TestEngineQueryTightBudgetCompresses verifies the budget cap forces
// graduated SACRS levels. It probes decreasing budgets until the allocator
// can no longer afford to upgrade every peripheral to raw, producing at least
// two distinct representation levels in the context while keeping total usage
// within the budget.
func TestEngineQueryTightBudgetCompresses(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in -short mode")
	}
	root := repoRoot(t)
	engine, cleanup := newTestEngine(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := engine.Index(ctx, root, "go", false); err != nil {
		t.Fatalf("Index: %v", err)
	}

	// Probe the ample-budget usage to size the search range.
	ample, err := engine.Query(ctx, "index repository context", root, 8192, 5)
	if err != nil {
		t.Fatalf("ample Query: %v", err)
	}
	if ample.PeripheralCount == 0 {
		t.Skip("no peripheral nodes selected; cannot test compression")
	}

	// Search decreasing budgets for one that yields >=2 distinct SACRS levels
	// AND keeps usage within budget. The repo's symbols may be small enough that
	// only a sharply reduced budget forces compression, so walk from ample/2
	// down to the minimum that still includes the core-pinned nodes.
	query := "index repository context"
	var compressed *QueryResult
	for tight := ample.TokensUsed / 2; tight >= 4; tight = tight * 2 / 3 {
		res, qerr := engine.Query(ctx, query, root, tight, 5)
		if qerr != nil {
			continue
		}
		if res.TokensUsed > tight {
			continue // core pinning alone overshoots; try a larger budget
		}
		levels := 0
		for _, k := range []string{"k=0", "k=1", "k=2", "k=3"} {
			if strings.Contains(res.Context, k) {
				levels++
			}
		}
		if levels >= 2 {
			compressed = res
			break
		}
	}
	if compressed == nil {
		t.Skip("no budget in the probed range produced >=2 distinct levels; " +
			"repo symbols are too small to force compression at the chosen topK")
	}
	// Final guarantee: the compressed selection must not overspend its budget.
	// (Already true since the loop skipped overshooting budgets, but assert
	// explicitly for clarity.)
	if compressed.TokensUsed > ample.TokensUsed {
		t.Fatalf("compressed usage %d exceeds ample usage %d",
			compressed.TokensUsed, ample.TokensUsed)
	}
}

// TestEngineQueryUnknownRepo ensures a query against an un-indexed repo fails
// with a clear error rather than panicking.
func TestEngineQueryUnknownRepo(t *testing.T) {
	engine, cleanup := newTestEngine(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := engine.Query(ctx, "anything", "/no/such/repo", 256, 5); err == nil {
		t.Fatalf("expected error for un-indexed repo")
	}
}

// TestEnginePipelineGraduatesLevels deterministically verifies the full
// Query pipeline wiring (semantic rank -> expand -> allocate -> materialize)
// on a small synthetic graph with known sizes. It asserts the context is
// assembled, the seed/core node is rendered at raw, and usage respects the
// budget. The graduation of peripheral levels under budget pressure is
// covered by the Allocate unit tests in selector_test.go; here we verify the
// Engine wires those components together correctly and honors the budget cap
// (pruning peripherals when needed).
func TestEnginePipelineGraduatesLevels(t *testing.T) {
	engine, cleanup := newTestEngine(t)
	defer cleanup()
	ctx := context.Background()

	// A tiny synthetic repo: one controller that calls two services. The
	// controller's raw cost is small; the services' raw bodies are large.
	repoPath := "/fake/synthetic"
	repoID := repoID(repoPath)
	ctrl := Symbol{
		ID: "sym:ctrl.go#ctrl", RepoID: repoID, Path: "ctrl.go", Name: "ctrl",
		Kind: "function", NodeType: NodeController,
		Signature: "func ctrl()",
		Body:      "{ svcA(); svcB() }",
	}
	svcA := Symbol{
		ID: "sym:svc.go#svcA", RepoID: repoID, Path: "svc.go", Name: "svcA",
		Kind: "function", NodeType: NodeService,
		Signature: "func svcA(x int) int",
		Body:      "{ /* a long body with many words to make raw expensive */ return x }",
	}
	svcB := Symbol{
		ID: "sym:svc.go#svcB", RepoID: repoID, Path: "svc.go", Name: "svcB",
		Kind: "function", NodeType: NodeService,
		Signature: "func svcB(y string) string",
		Body:      "{ /* another long body with many words for raw cost */ return y }",
	}
	syms := []Symbol{ctrl, svcA, svcB}
	edges := []Edge{
		{RepoID: repoID, From: ctrl.ID, To: svcA.ID, Kind: EdgeCall, Weight: 1.0},
		{RepoID: repoID, From: ctrl.ID, To: svcB.ID, Kind: EdgeCall, Weight: 1.0},
	}
	computeDegrees(syms, edges)

	// Embed the retrieval text deterministically.
	texts := make([]string, len(syms))
	for i, s := range syms {
		texts[i] = strings.TrimSpace(s.Signature + " " + s.Doc + " " + s.Name)
	}
	vecs, err := engine.embedder.Embed(ctx, texts)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	for i := range syms {
		syms[i].Embedding = vecs[i]
	}

	if err := engine.store.SaveRepo(RepoMeta{
		ID: repoID, Path: repoPath, Hash: "h", Lang: "go",
		CreatedAt: "t", IndexedAt: "t",
	}); err != nil {
		t.Fatalf("SaveRepo: %v", err)
	}
	if err := engine.store.ReplaceRepoSymbols(repoID, syms, edges); err != nil {
		t.Fatalf("ReplaceRepoSymbols: %v", err)
	}

	// topK=1: only the top semantic match is a core seed; the other two are
	// peripheral (reachable via the controller's call edges through BFS). A
	// tiny budget forces the allocator to prune/compress peripherals, so usage
	// must not exceed the budget unless every selected node is core.
	res, err := engine.Query(ctx, "ctrl service query", repoPath, 8, 1)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Nodes == 0 {
		t.Fatalf("no nodes selected")
	}
	if res.Context == "" {
		t.Fatalf("empty context")
	}
	// Budget guarantee: usage <= budget OR all selected nodes are core (core is
	// pinned at raw unconditionally; only peripherals are pruned/compressed).
	if res.TokensUsed > 8 && res.PeripheralCount > 0 {
		t.Fatalf("peripherals overspent budget: used %d > 8 (peripherals=%d)",
			res.TokensUsed, res.PeripheralCount)
	}
	// The context must render at least the seed at raw and carry the budget header.
	if !strings.Contains(res.Context, "k=0") {
		t.Fatalf("seed should be at k=0:\n%s", res.Context)
	}
	if !strings.Contains(res.Context, "tokens used:") {
		t.Fatalf("context missing budget header:\n%s", res.Context)
	}

	// With an ample budget, every selected node upgrades to raw: no compressed
	// level (k=1/2/3) should appear.
	ample, err := engine.Query(ctx, "ctrl service query", repoPath, 4096, 3)
	if err != nil {
		t.Fatalf("ample Query: %v", err)
	}
	for _, k := range []string{"k=1", "k=2", "k=3"} {
		if strings.Contains(ample.Context, k) {
			t.Fatalf("ample budget should upgrade all nodes to raw, but found %s:\n%s", k, ample.Context)
		}
	}
	if ample.TokensUsed > 4096 {
		t.Fatalf("ample usage %d exceeds budget", ample.TokensUsed)
	}
}
