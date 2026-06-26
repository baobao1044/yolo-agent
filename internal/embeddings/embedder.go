// Package embeddings provides text embedding backends used by the CORE
// retrieval engine and (later) the conversation memory system.
//
// The Embedder abstraction is shared core: it is independent of the Code-RAG
// domain so that both corerag and the memory package can reuse the same
// backends (local ONNX, OpenAI-compatible API, Ollama).
package embeddings

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// Embedder converts batches of text into fixed-dimensional float32 vectors.
// Implementations must be safe for concurrent use.
type Embedder interface {
	// Embed produces one vector per input text, preserving order.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Dim returns the dimensionality of the vectors this embedder produces.
	Dim() int
}

// TokenCounter estimates the token cost of a string. The greedy allocator
// only requires *relative* cost ordering, so a cheap heuristic suffices; a
// real tokenizer may be injected for tighter budgets (Spec 2).
type TokenCounter interface {
	Count(text string) int
}

// Provider names the available embedding backends.
type Provider string

const (
	// ProviderONNX runs an ONNX model in-process (default; keeps source local).
	ProviderONNX  Provider = "onnx"
	// ProviderAPI calls an OpenAI-compatible /v1/embeddings endpoint.
	ProviderAPI   Provider = "api"
	// ProviderOllama calls a local Ollama /api/embeddings endpoint.
	ProviderOllama Provider = "ollama"
	// ProviderMock is a deterministic hash-based embedder for tests.
	ProviderMock  Provider = "mock"
)

// EmbeddingConfig configures the embedding backend. It is shared between the
// corerag config and (later) the memory config.
type EmbeddingConfig struct {
	// Provider selects the backend: "onnx" (default), "api", "ollama", "mock".
	Provider Provider `yaml:"provider"`
	// Fallback is used when the primary provider fails to initialize.
	// Defaults to "api" when empty.
	Fallback Provider `yaml:"fallback"`

	// API backend settings.
	BaseURL string `yaml:"base_url"` // OpenAI-compatible endpoint; empty = inherit LLM base URL
	APIKey  string `yaml:"api_key"`  // empty = inherit YOLO_API_KEY at call time
	Model   string `yaml:"model"`     // embedding model name

	// ONNX backend settings.
	ModelPath string `yaml:"model_path"` // path to .onnx file
	// LibPath overrides the onnxruntime shared library location (else env YOLO_ONNX_LIB).
	LibPath string `yaml:"lib_path"`

	// Ollama backend settings.
	OllamaURL string `yaml:"ollama_url"` // default http://localhost:11434
	OllamaModel string `yaml:"ollama_model"`

	// Dim is the embedding dimension. If 0, the provider reports its own.
	Dim int `yaml:"dim"`
}

// NewEmbedder constructs the configured embedder, falling back to the
// secondary provider if the primary cannot initialize. This guarantees the
// engine can always run (matching the soft-failure convention used by MCP and
// the browser in cmd/agent/main.go).
func NewEmbedder(cfg EmbeddingConfig, llm LLMEmbedder, logger *slog.Logger) (Embedder, error) {
	if logger == nil {
		logger = slog.Default()
	}
	primary, err := buildProvider(cfg, llm, logger)
	if err == nil {
		return primary, nil
	}

	fallback := cfg.Fallback
	if fallback == "" {
		fallback = ProviderAPI
	}
	logger.Warn("primary embedding provider failed; trying fallback",
		"primary", cfg.Provider, "error", err, "fallback", fallback)

	fbCfg := cfg
	fbCfg.Provider = fallback
	secondary, fbErr := buildProvider(fbCfg, llm, logger)
	if fbErr != nil {
		return nil, fmt.Errorf("embedding fallback %q also failed: %w (primary: %v)", fallback, fbErr, err)
	}
	return secondary, nil
}

// buildProvider constructs a single embedder for the given provider name.
func buildProvider(cfg EmbeddingConfig, llm LLMEmbedder, logger *slog.Logger) (Embedder, error) {
	switch cfg.Provider {
	case ProviderMock:
		dim := cfg.Dim
		if dim <= 0 {
			dim = 32
		}
		return NewMockEmbedder(dim), nil
	case ProviderAPI:
		if llm == nil {
			return nil, fmt.Errorf("api embedder requires an LLM client")
		}
		return NewAPIEmbedder(llm, cfg.Model, cfg.Dim), nil
	case ProviderOllama:
		url := cfg.OllamaURL
		if url == "" {
			url = "http://localhost:11434"
		}
		model := cfg.OllamaModel
		if model == "" {
			model = cfg.Model
		}
		return NewOllamaEmbedder(url, model, cfg.Dim, logger), nil
	case ProviderONNX:
		return NewONNXEmbedder(cfg, logger)
	case "":
		return nil, fmt.Errorf("embedding provider is empty")
	default:
		return nil, fmt.Errorf("unknown embedding provider %q", cfg.Provider)
	}
}

// LLMEmbedder is the narrow interface an API-backed embedder needs from the
// LLM client: the ability to create embeddings. llm.OpenAIClient satisfies it.
type LLMEmbedder interface {
	CreateEmbeddings(ctx context.Context, texts []string, model string) ([][]float32, error)
}

// HeuristicTokenCounter estimates token count from word count. Code tokenizes
// at roughly 0.75 tokens per whitespace-separated field; this is only an
// approximation but preserves the relative ordering the greedy allocator
// relies on. Replace with a real tokenizer in Spec 2 for tighter budgets.
type HeuristicTokenCounter struct{}

// Count estimates the number of tokens in text.
func (HeuristicTokenCounter) Count(text string) int {
	if text == "" {
		return 0
	}
	// ~1.33 tokens per field (inverse of 0.75 tokens/field).
	fields := len(strings.Fields(text))
	return (fields*4 + 2) / 3
}
