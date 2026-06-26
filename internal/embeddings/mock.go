package embeddings

import (
	"context"
	"hash/fnv"
	"math"
)

// MockEmbedder produces deterministic, low-dimensional vectors by hashing
// the input text. It is intended for tests and offline development where no
// model is available. Vectors are L2-normalized so cosine similarity behaves
// sensibly.
type MockEmbedder struct {
	dim int
}

// NewMockEmbedder creates a deterministic mock embedder producing the given
// number of dimensions.
func NewMockEmbedder(dim int) *MockEmbedder {
	if dim <= 0 {
		dim = 32
	}
	return &MockEmbedder{dim: dim}
}

// Embed returns one normalized hash-vector per input text.
func (m *MockEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = m.vectorize(t)
	}
	return out, nil
}

// Dim returns the embedding dimensionality.
func (m *MockEmbedder) Dim() int { return m.dim }

// vectorize maps text to a fixed-length, L2-normalized float32 vector via
// FNV hashing. Identical texts produce identical vectors; similar texts are
// uncorrelated (this is fine for tests that only need deterministic retrieval).
func (m *MockEmbedder) vectorize(text string) []float32 {
	v := make([]float32, m.dim)
	for i := 0; i < m.dim; i++ {
		h := fnv.New32a()
		_, _ = h.Write([]byte(text))
		_, _ = h.Write([]byte{byte(i), byte(i >> 8)})
		// Map uint32 to {-1,+1}-ish signed value.
		bit := h.Sum32() & 1
		if bit == 0 {
			v[i] = -1
		} else {
			v[i] = 1
		}
	}
	normalize(v)
	return v
}

// normalize divides v by its L2 norm in place; a zero vector is left as-is.
func normalize(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return
	}
	inv := 1.0 / math.Sqrt(sum)
	for i := range v {
		v[i] = float32(float64(v[i]) * inv)
	}
}
