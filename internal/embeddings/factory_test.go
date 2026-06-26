package embeddings

import (
	"context"
	"log/slog"
	"testing"
)

func TestNewEmbedderMock(t *testing.T) {
	e, err := NewEmbedder(EmbeddingConfig{Provider: ProviderMock, Dim: 8}, nil, slog.Default())
	if err != nil {
		t.Fatalf("NewEmbedder mock: %v", err)
	}
	v, err := e.Embed(context.Background(), []string{"x"})
	if err != nil || len(v) != 1 || e.Dim() != 8 {
		t.Fatalf("mock embed failed: v=%v err=%v dim=%d", v, err, e.Dim())
	}
}

func TestNewEmbedderFallsBack(t *testing.T) {
	// ONNX without a model fails init -> fallback to api with a fake LLM.
	e, err := NewEmbedder(EmbeddingConfig{
		Provider: ProviderONNX,
		Fallback: ProviderAPI,
		Model:    "test-embed",
	}, &fakeLLMEmbedder{dim: 32}, slog.Default())
	if err != nil {
		t.Fatalf("expected fallback to api, got err: %v", err)
	}
	v, err := e.Embed(context.Background(), []string{"hello"})
	if err != nil || len(v) != 1 || len(v[0]) != 32 {
		t.Fatalf("api fallback embed failed: v=%v err=%v", v, err)
	}
}

func TestNewEmbedderUnknownProvider(t *testing.T) {
	_, err := NewEmbedder(EmbeddingConfig{Provider: "bogus"}, nil, slog.Default())
	if err == nil {
		t.Fatalf("expected error for unknown provider")
	}
}
