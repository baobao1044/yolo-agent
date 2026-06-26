package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// OllamaEmbedder calls a local Ollama server's /api/embeddings endpoint. This
// keeps source code on the local machine (no provider round-trip) without
// requiring a native ONNX runtime inside the binary.
type OllamaEmbedder struct {
	baseURL  string
	model    string
	configDim int
	http     *http.Client
	logger   *slog.Logger
	mu       sync.RWMutex
	knownDim int
}

// NewOllamaEmbedder creates an Ollama-backed embedder.
func NewOllamaEmbedder(baseURL, model string, dim int, logger *slog.Logger) *OllamaEmbedder {
	return &OllamaEmbedder{
		baseURL:   baseURL,
		model:     model,
		configDim: dim,
		http:      &http.Client{Timeout: 60 * time.Second},
		logger:    logger,
	}
}

type ollamaEmbedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaEmbedResponse struct {
	Embedding []float32 `json:"embedding"`
}

// Embed calls the Ollama endpoint once per text (the public API is
// single-prompt). Errors on individual texts are collected and surfaced as a
// single error to keep the batch contract simple.
func (o *OllamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		body, err := json.Marshal(ollamaEmbedRequest{Model: o.model, Prompt: t})
		if err != nil {
			return nil, fmt.Errorf("marshal ollama request: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/embeddings", bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("build ollama request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := o.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("ollama embeddings request: %w", err)
		}
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("ollama embeddings returned %d: %s", resp.StatusCode, string(respBody))
		}

		var er ollamaEmbedResponse
		if err := json.Unmarshal(respBody, &er); err != nil {
			return nil, fmt.Errorf("parse ollama response: %w", err)
		}
		out[i] = er.Embedding

		if i == 0 && len(er.Embedding) > 0 {
			o.mu.Lock()
			o.knownDim = len(er.Embedding)
			o.mu.Unlock()
		}
	}
	return out, nil
}

// Dim returns the embedding dimensionality.
func (o *OllamaEmbedder) Dim() int {
	if o.configDim > 0 {
		return o.configDim
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.knownDim
}
