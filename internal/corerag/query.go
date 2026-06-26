package corerag

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/baobg/yolo-agent/internal/tools"
)

// RepoQueryTool implements the `repo_query` tool: retrieve budget-aware
// context for a natural-language query against an indexed repo. Runs the
// CORE pipeline: semantic seed retrieval -> graph expansion (S>=theta) ->
// core/peripheral split -> greedy MCKP allocation -> SACRS materialization ->
// assembled context with citations.
type RepoQueryTool struct {
	engine *Engine
}

// NewRepoQueryTool constructs the repo_query tool.
func NewRepoQueryTool(engine *Engine) *RepoQueryTool { return &RepoQueryTool{engine: engine} }

// Name returns the tool identifier.
func (t *RepoQueryTool) Name() string { return "repo_query" }

// Description returns the tool description seen by the LLM.
func (t *RepoQueryTool) Description() string {
	return "Retrieve budget-aware context from an indexed repository for a natural-language query. Returns assembled code context at graduated representation levels (full source, signature, stub) chosen by the CORE budget-aware allocator, with per-symbol citations and token usage. The repository must be indexed first with index_repo."
}

// Schema returns the JSON Schema for the tool's parameters.
func (t *RepoQueryTool) Schema() tools.ToolSchema {
	return tools.ToolSchema{
		Type: "object",
		Properties: map[string]tools.SchemaProperty{
			"action": {Type: "string", Description: "The action to perform. Must be \"query\".", Enum: []string{"query"}, Default: "query"},
			"query":  {Type: "string", Description: "Natural-language question about the repository."},
			"repo":   {Type: "string", Description: "Absolute path of the indexed repository (or its repo id)."},
			"budget": {Type: "number", Description: "Token budget override for this query. Defaults to the configured budget."},
			"top_k":  {Type: "number", Description: "Number of semantic seed symbols to retrieve. Defaults to 8."},
		},
		Required: []string{"query", "repo"},
	}
}

// Execute runs the query action.
func (t *RepoQueryTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Action string `json:"action"`
		Query  string `json:"query"`
		Repo   string `json:"repo"`
		Budget int    `json:"budget"`
		TopK   int    `json:"top_k"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("parse repo_query args: %w", err)
	}
	if p.Query == "" {
		return nil, fmt.Errorf("repo_query: query is required")
	}
	if p.Repo == "" {
		return nil, fmt.Errorf("repo_query: repo is required")
	}
	if p.Action == "" {
		p.Action = "query"
	}
	if p.Action != "query" {
		return nil, fmt.Errorf("repo_query: unsupported action %q", p.Action)
	}
	budget := p.Budget
	if budget <= 0 {
		budget = t.engine.cfg.Budget
	}
	topK := p.TopK
	if topK <= 0 {
		topK = 8
	}
	return t.engine.Query(ctx, p.Query, p.Repo, budget, topK)
}

// QueryResult is returned by the query action.
type QueryResult struct {
	RepoID          string `json:"repo_id"`
	Context         string `json:"context"`
	TokensUsed      int    `json:"tokens_used"`
	Nodes           int    `json:"nodes"`
	CoreCount       int    `json:"core_count"`
	PeripheralCount int    `json:"peripheral_count"`
	Seeds           []string `json:"seeds"`
}

// Query runs the CORE retrieval pipeline for a single query.
func (e *Engine) Query(ctx context.Context, query, repoRef string, budget, topK int) (*QueryResult, error) {
	// Resolve repo id (accept either a repo:... id or a raw path).
	repoID := repoRef
	repo, err := e.store.GetRepo(repoID)
	if err != nil || repo == nil {
		if r, rerr := e.store.FindRepoByPath(repoRef); rerr == nil && r != nil {
			repo = r
			repoID = r.ID
		}
	}
	if repo == nil {
		return nil, fmt.Errorf("repository %q is not indexed; call index_repo first", repoRef)
	}

	symbols, err := e.store.LoadSymbols(repoID)
	if err != nil {
		return nil, fmt.Errorf("load symbols: %w", err)
	}
	edges, err := e.store.LoadEdges(repoID)
	if err != nil {
		return nil, fmt.Errorf("load edges: %w", err)
	}
	graph := NewGraph(symbols, edges)

	// 1. Semantic retrieval: embed the query and rank symbols by cosine.
	queryEmb, err := e.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(queryEmb) == 0 || len(queryEmb[0]) == 0 {
		return nil, fmt.Errorf("empty query embedding")
	}
	qv := queryEmb[0]

	type scored struct {
		sym Symbol
		sem float64
	}
	ranked := make([]scored, 0, len(symbols))
	for _, s := range symbols {
		if len(s.Embedding) == 0 {
			continue
		}
		ranked = append(ranked, scored{s, Semantic(qv, s.Embedding)})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].sem > ranked[j].sem })

	if topK > len(ranked) {
		topK = len(ranked)
	}
	seeds := make([]string, 0, topK)
	for i := 0; i < topK; i++ {
		seeds = append(seeds, ranked[i].sym.ID)
	}

	// 2. Graph expansion with the relevance scorer (S>=theta).
	scorer := NewRelevanceScorer(e.cfg.Relevance)
	maxOut := graph.MaxOutDegree()
	seedSet := make(map[string]bool, len(seeds))
	for _, id := range seeds {
		seedSet[id] = true
	}
	scoreFn := func(id string) float64 {
		sym, ok := graph.Symbol(id)
		if !ok {
			return 0
		}
		sem := Semantic(qv, sym.Embedding)
		// Edge weight: average weight of incoming edges.
		var ew float64
		in := graph.reverseEdges(id)
		if len(in) > 0 {
			sum := 0.0
			for _, e := range in {
				sum += EdgeWeight(e)
			}
			ew = sum / float64(len(in))
		}
		return scorer.Score(ScoreInput{
			Semantic:       sem,
			EdgeWeight:     ew,
			Centrality:     Centrality(sym.OutDegree, maxOut),
			UtilityPenalty: UtilityPenalty(sym.InDegree),
		})
	}
	expanded := graph.Expand(seeds, e.cfg.Depth, e.cfg.Relevance.Theta, scoreFn)

	// 3. Build node contexts: seeds are core (pinned k=0); others peripheral.
	var nodes []NodeCtx
	for _, id := range expanded {
		sym, ok := graph.Symbol(id)
		if !ok {
			continue
		}
		s := scoreFn(id)
		nodes = append(nodes, NodeCtx{Symbol: sym, Score: s, Core: seedSet[id]})
	}

	// 4. Greedy MCKP allocation.
	priors := NewPriorTable(e.cfg.Priors)
	sel := Allocate(nodes, budget, priors, e.tokenizer)

	// 5. Materialize each selected node at its level and assemble context.
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# Repository context for: %s\n", query))
	b.WriteString(fmt.Sprintf("# Repo: %s | tokens used: %d / %d\n\n", repo.Path, sel.TokensUsed, budget))
	includedIDs := make([]string, 0, len(sel.Levels))
	for id := range sel.Levels {
		includedIDs = append(includedIDs, id)
	}
	sort.Strings(includedIDs)
	for _, id := range includedIDs {
		sym, _ := graph.Symbol(id)
		level := sel.Levels[id]
		rep := Materialize(sym, level)
		b.WriteString(fmt.Sprintf("\n## %s [%s, k=%d]\n```%s\n%s\n```\n",
			sym.Name, sym.NodeType, level, repo.Lang, rep))
	}

	return &QueryResult{
		RepoID:          repoID,
		Context:         b.String(),
		TokensUsed:      sel.TokensUsed,
		Nodes:           sel.Nodes,
		CoreCount:       sel.CoreCount,
		PeripheralCount: sel.PeripheralCount,
		Seeds:           seeds,
	}, nil
}

// reverseEdges returns edges entering the given symbol (for edge-weight calc).
func (g *Graph) reverseEdges(id string) []Edge {
	return g.reverse[id]
}
