//go:build !cgo

package embeddings

import (
	"fmt"
	"log/slog"
)

// NewONNXEmbedder returns an error when the binary was compiled without cgo,
// because the onnxruntime_go binding requires cgo. The embeddings factory
// (NewEmbedder) catches this and falls back to the API provider, so the agent
// still runs.
func NewONNXEmbedder(cfg EmbeddingConfig, logger *slog.Logger) (Embedder, error) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.Warn("ONNX embedder unavailable: binary built without cgo", "fallback", cfg.Fallback)
	return nil, fmt.Errorf("ONNX embedder requires cgo; rebuild with CGO_ENABLED=1 and the onnxruntime shared library")
}
