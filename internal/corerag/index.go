package corerag

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/baobg/yolo-agent/internal/tools"
)

// IndexRepoTool implements the `index_repo` tool: walk a repository, parse it
// into symbols + edges, classify node types, compute in/out-degree, embed the
// retrieval representation, and persist to SQLite. The repo is then queryable
// via repo_query.
type IndexRepoTool struct {
	engine *Engine
}

// NewIndexRepoTool constructs the index_repo tool.
func NewIndexRepoTool(engine *Engine) *IndexRepoTool { return &IndexRepoTool{engine: engine} }

// Name returns the tool identifier.
func (t *IndexRepoTool) Name() string { return "index_repo" }

// Description returns the tool description seen by the LLM.
func (t *IndexRepoTool) Description() string {
	return "Index a source code repository for the CORE Code-RAG engine. Walks the directory, parses symbols and call/import edges, classifies node types, computes graph degrees, and embeds symbols for retrieval. After indexing, use repo_query to retrieve budget-aware context from the repo."
}

// Schema returns the JSON Schema for the tool's parameters.
func (t *IndexRepoTool) Schema() tools.ToolSchema {
	return tools.ToolSchema{
		Type: "object",
		Properties: map[string]tools.SchemaProperty{
			"action":   {Type: "string", Description: "The action to perform. Must be \"index\".", Enum: []string{"index"}, Default: "index"},
			"path":     {Type: "string", Description: "Absolute path to the repository root to index."},
			"language": {Type: "string", Description: "Language hint (e.g. \"go\"). If omitted, detected from file extensions."},
			"force":    {Type: "boolean", Description: "Re-index even if the repo hash is unchanged.", Default: false},
		},
		Required: []string{"path"},
	}
}

// Execute runs the index action.
func (t *IndexRepoTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Action   string `json:"action"`
		Path     string `json:"path"`
		Language string `json:"language"`
		Force    bool   `json:"force"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("parse index_repo args: %w", err)
	}
	if p.Path == "" {
		return nil, fmt.Errorf("index_repo: path is required")
	}
	if p.Action == "" {
		p.Action = "index"
	}
	if p.Action != "index" {
		return nil, fmt.Errorf("index_repo: unsupported action %q", p.Action)
	}
	return t.engine.Index(ctx, p.Path, p.Language, p.Force)
}

// IndexResult is returned by the index action.
type IndexResult struct {
	RepoID     string `json:"repo_id"`
	Path       string `json:"path"`
	Language   string `json:"language"`
	Files      int    `json:"files"`
	Symbols    int    `json:"symbols"`
	Edges      int    `json:"edges"`
	Hash       string `json:"hash"`
	DurationMs int64  `json:"duration_ms"`
}

// Index walks the repo, parses, classifies, embeds, and persists. It is the
// implementation behind the index_repo tool and may also be called directly.
func (e *Engine) Index(ctx context.Context, repoPath, langHint string, force bool) (*IndexResult, error) {
	start := time.Now()
	repoPath = filepath.Clean(repoPath)

	// Detect language and select a parser.
	lang := langHint
	if lang == "" {
		lang = detectLanguage(repoPath)
	}
	p, ok := e.parsers[lang]
	if !ok {
		return nil, fmt.Errorf("no parser for language %q (available: %s)", lang, strings.Join(parserNames(e.parsers), ", "))
	}

	// Walk files for the language.
	files, err := collectSourceFiles(repoPath, lang)
	if err != nil {
		return nil, fmt.Errorf("collect source files: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no %s source files found under %s", lang, repoPath)
	}

	// Hash the file set so we can skip unchanged repos.
	hash, err := hashFiles(files)
	if err != nil {
		return nil, fmt.Errorf("hash repo: %w", err)
	}

	repoID := repoID(repoPath)
	if existing, _ := e.store.FindRepoByPath(repoPath); existing != nil && existing.Hash == hash && !force {
		// Unchanged repo: report the persisted counts so the no-op result is
		// accurate rather than reporting zero symbols/edges.
		symN, _ := e.store.CountSymbols(existing.ID)
		edgeN, _ := e.store.CountEdges(existing.ID)
		return &IndexResult{
			RepoID: existing.ID, Path: existing.Path, Language: existing.Lang,
			Files: len(files), Symbols: symN, Edges: edgeN,
			Hash: hash, DurationMs: time.Since(start).Milliseconds(),
		}, nil
	}

	var allSymbols []Symbol
	var allEdges []Edge
	parsed := 0
	for _, fpath := range files {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		src, err := readFile(fpath)
		if err != nil {
			e.logger.Warn("skip unreadable file", "path", fpath, "error", err)
			continue
		}
		rel := relPath(repoPath, fpath)
		syms, edges, err := p.Extract(ctx, rel, src)
		if err != nil {
			e.logger.Warn("parse failed", "path", rel, "error", err)
			continue
		}
		for i := range syms {
			syms[i].RepoID = repoID
			syms[i].NodeType = Classify(syms[i], lang)
		}
		for i := range edges {
			edges[i].RepoID = repoID
		}
		allSymbols = append(allSymbols, syms...)
		allEdges = append(allEdges, edges...)
		parsed++
	}

	// Resolve synthetic call/import targets to known symbol IDs and compute
	// in/out-degree.
	allEdges = resolveEdges(allSymbols, allEdges)
	computeDegrees(allSymbols, allEdges)

	// Embed the retrieval representation (Signature + " " + Doc + " " + Name).
	if e.embedder != nil {
		texts := make([]string, len(allSymbols))
		for i, s := range allSymbols {
			texts[i] = strings.TrimSpace(s.Signature + " " + s.Doc + " " + s.Name)
		}
		vecs, err := e.embedder.Embed(ctx, texts)
		if err != nil {
			e.logger.Warn("embedding failed; symbols stored without embeddings", "error", err)
		} else if len(vecs) == len(allSymbols) {
			for i := range allSymbols {
				allSymbols[i].Embedding = vecs[i]
			}
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if err := e.store.SaveRepo(RepoMeta{
		ID: repoID, Path: repoPath, Hash: hash, Lang: lang, CreatedAt: now, IndexedAt: now,
	}); err != nil {
		return nil, fmt.Errorf("save repo meta: %w", err)
	}
	if err := e.store.ReplaceRepoSymbols(repoID, allSymbols, allEdges); err != nil {
		return nil, fmt.Errorf("persist symbols: %w", err)
	}

	return &IndexResult{
		RepoID: repoID, Path: repoPath, Language: lang,
		Files: parsed, Symbols: len(allSymbols), Edges: len(allEdges),
		Hash: hash, DurationMs: time.Since(start).Milliseconds(),
	}, nil
}
