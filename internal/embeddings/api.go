package embeddings

import (
	"context"
	"fmt"
	"sync"
)

// APIEmbedder calls an OpenAI-compatible /v1/embeddings endpoint via the
// injected LLM client. It is the configured fallback when a local ONNX model
// is unavailable.
type APIEmbedder struct {
	llm     LLMEmbedder
	model   string
	configDim int
	mu      sync.RWMutex
	knownDim int
}

// NewAPIEmbedder creates an API-backed embedder. model is the embedding model
// name; dim is the expected dimension (0 = discover from first response).
func NewAPIEmbedder(llm LLMEmbedder, model string, dim int) *APIEmbedder {
	return &APIEmbedder{llm: llm, model: model, configDim: dim}
}

// Embed delegates to the LLM client's embedding endpoint.
func (a *APIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	vecs, err := a.llm.CreateEmbeddings(ctx, texts, a.model)
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("embedding endpoint returned no vectors")
	}

	// Discover dimension from the first response if not configured.
	a.mu.RLock()
	kd := a.knownDim
	a.mu.RUnlock()
	if kd == 0 {
		first := 0
		for first < len(vecs) && len(vecs[first]) == 0 {
			first++
		}
		if first < len(vecs) {
			a.mu.Lock()
			a.knownDim = len(vecs[first])
			a.mu.Unlock()
		}
	}
	return vecs, nil
}

// Dim returns the embedding dimensionality.
func (a *APIEmbedder) Dim() int {
	if a.configDim > 0 {
		return a.configDim
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.knownDim
}
