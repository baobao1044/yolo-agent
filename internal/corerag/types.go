// Package corerag implements the CORE (Context Optimization and Retrieval
// Engine) framework: a budget-aware representation selector for
// repository-scale code reasoning. It indexes a codebase into a call/import
// graph, classifies symbols by architectural role, and at query time assembles
// a token-budgeted context by choosing graduated representation levels per
// symbol (full source -> signature -> stub).
//
// This package is the Code-RAG engine (Spec 1). The greedy selector and
// embedding backends live in internal/embeddings and internal/corerag so the
// conversation memory system (Spec 4) can reuse them later.
package corerag

import (
	"context"
	"log/slog"

	"github.com/baobg/yolo-agent/internal/embeddings"
	"github.com/baobg/yolo-agent/internal/tools"
)

// Parser extracts symbols and edges from a single source file. The interface
// lives in corerag (not the parser subpackage) to avoid an import cycle:
// implementations in internal/corerag/parser import corerag for the Symbol
// and Edge types, so the interface they satisfy must be defined here.
type Parser interface {
	// Name returns the language this parser handles (e.g. "go", "python").
	Name() string
	// Extract parses src (at the given path) into symbols and edges.
	Extract(ctx context.Context, path, src string) ([]Symbol, []Edge, error)
}

// NodeType classifies a symbol by its architectural role, per CORE doc Table 1.
// The compression prior R(k, Type) and the SACRS representation family are
// indexed by this type.
type NodeType string

const (
	// NodeController handles framework entry points (HTTP handlers, route
	// callbacks). Annotations/decorators carry architectural semantics.
	NodeController NodeType = "Controller"
	// NodeDTO is a data definition (models, schemas, entities) whose field
	// types are reasoning-essential.
	NodeDTO NodeType = "DTO"
	// NodeService orchestrates business logic via calls to other symbols.
	NodeService NodeType = "Service"
	// NodeUtility is a peripheral helper (logging, validation, formatting)
	// whose implementation body carries little reasoning value.
	NodeUtility NodeType = "Utility"
	// NodeOther is the fallback for symbols that do not match a known role.
	NodeOther NodeType = "Other"
)

// Level is a SACRS compression level, k in {0,1,2,3}:
//
//	0 = raw source, 1 = signature+docs, 2 = interface signature, 3 = stub.
type Level int

const (
	// LevelRaw is the full source representation (information ceiling).
	LevelRaw Level = 0
	// LevelSigDocs keeps signature + return type + API docs, drops body.
	LevelSigDocs Level = 1
	// LevelInterface keeps only the interface signature, drops docs and body.
	LevelInterface Level = 2
	// LevelStub keeps the symbol name only.
	LevelStub Level = 3
)

// Symbol is a code symbol extracted from a repository file.
type Symbol struct {
	ID         string   `json:"id"`
	RepoID     string   `json:"repo_id"`
	Path       string   `json:"path"`
	Name       string   `json:"name"`        // e.g. "IndexRepoTool" or "Handler.ServeHTTP"
	Kind       string   `json:"kind"`        // function|method|type|class|method-decl
	NodeType   NodeType `json:"node_type"`
	Signature  string   `json:"signature"`  // e.g. "func (t *Tool) Execute(ctx, args) (any, error)"
	Doc        string   `json:"doc"`         // leading doc comment
	Body       string   `json:"body"`        // implementation text
	ByteStart  int      `json:"byte_start"`
	ByteEnd    int      `json:"byte_end"`
	Embedding  []float32 `json:"-"`          // retrieval embedding (Signature+Doc+Name)
	InDegree   int      `json:"in_degree"`
	OutDegree  int      `json:"out_degree"`
}

// EdgeKind names a relationship in the code structure graph.
type EdgeKind string

const (
	// EdgeCall is a function/method call from one symbol to another.
	EdgeCall EdgeKind = "call"
	// EdgeImport is a file-level import / dependency relationship.
	EdgeImport EdgeKind = "import"
	// EdgeImplements links a concrete type to an interface it satisfies.
	EdgeImplements EdgeKind = "implements"
	// EdgeTypeRef links a symbol to a type it references (field, param, return).
	EdgeTypeRef EdgeKind = "type_ref"
)

// Edge is a directed relationship in the code structure graph G = (V, E).
type Edge struct {
	ID      int64    `json:"id"`
	RepoID  string   `json:"repo_id"`
	From    string   `json:"from"`  // source symbol ID
	To      string   `json:"to"`    // target symbol ID
	Kind    EdgeKind `json:"kind"`
	Weight  float64  `json:"weight"` // framework-aware weight (1.5/1.0/0.5)
}

// RepoMeta records an indexed repository.
type RepoMeta struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Hash      string `json:"hash"`       // content hash of indexed files
	Lang      string `json:"lang"`       // detected language ("go","python",...)
	CreatedAt string `json:"created_at"`
	IndexedAt string `json:"indexed_at"`
}

// Engine ties together the store, embedder, config, and pipeline components
// for both index_repo and repo_query. It is the single dependency injected
// into the two tools.
type Engine struct {
	store     *Store
	embedder  embeddings.Embedder
	tokenizer embeddings.TokenCounter
	cfg       CoreRAGConfig
	logger    *slog.Logger
	parsers   map[string]Parser // keyed by language ("go","python",...)
}

// NewEngine constructs the engine, selecting parsers by language.
func NewEngine(store *Store, embedder embeddings.Embedder, cfg CoreRAGConfig, logger *slog.Logger) *Engine {
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{
		store:     store,
		embedder:  embedder,
		tokenizer: embeddings.HeuristicTokenCounter{},
		cfg:       cfg,
		logger:    logger,
		parsers:   buildParsers(logger),
	}
}

// buildParsers returns the language -> Parser map. The Go parser is pure stdlib
// and always available; tree-sitter parsers (Python/TS/JS/Java) are only
// included when cgo is enabled (ts_parsers.go is build-tagged).
func buildParsers(logger *slog.Logger) map[string]Parser {
	parsers := map[string]Parser{
		"go": &GoParser{},
	}
	if ts := newTreeSitterParsers(logger); ts != nil {
		for lang, p := range ts {
			parsers[lang] = p
		}
	}
	return parsers
}

// The two tools live in index.go and query.go and implement tools.Tool via the
// structural interface (same pattern as agent.OrchestrateTool).
var _ tools.Tool = (*IndexRepoTool)(nil)
var _ tools.Tool = (*RepoQueryTool)(nil)
