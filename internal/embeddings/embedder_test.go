package embeddings

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestMockEmbedderDeterministic(t *testing.T) {
	m := NewMockEmbedder(16)
	a, err := m.Embed(context.Background(), []string{"hello world", "hello world", "different"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(a) != 3 {
		t.Fatalf("want 3 vectors, got %d", len(a))
	}
	if m.Dim() != 16 {
		t.Fatalf("Dim = %d, want 16", m.Dim())
	}
	// Identical texts must produce identical vectors.
	if !vectorsEqual(a[0], a[1]) {
		t.Fatalf("identical texts produced different vectors")
	}
	// All vectors must be L2-normalized.
	for i, v := range a {
		if n := l2norm(v); n < 0.999 || n > 1.001 {
			t.Fatalf("vector %d not normalized (norm=%v)", i, n)
		}
	}
}

func TestMockEmbedderEmpty(t *testing.T) {
	m := NewMockEmbedder(8)
	v, err := m.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(v) != 0 {
		t.Fatalf("want empty result for empty input, got %v", v)
	}
}

func TestMockEmbedderDefaultDim(t *testing.T) {
	m := NewMockEmbedder(0)
	if m.Dim() != 32 {
		t.Fatalf("Dim = %d, want default 32", m.Dim())
	}
}

func TestHeuristicTokenCounter(t *testing.T) {
	c := HeuristicTokenCounter{}
	if n := c.Count(""); n != 0 {
		t.Fatalf("empty = %d, want 0", n)
	}
	// "func foo() int" -> 3 whitespace-separated fields (foo() is one field)
	// -> (3*4+2)/3 = 14/3 = 4 tokens.
	if n := c.Count("func foo() int"); n != 4 {
		t.Fatalf("Count = %d, want 4", n)
	}
	// Monotonic-ish: more fields => at least as many tokens.
	short := c.Count("a b c")
	long := c.Count("a b c d e f g h")
	if long < short {
		t.Fatalf("longer text should not count fewer tokens: %d < %d", long, short)
	}
}

// fakeLLMEmbedder implements LLMEmbedder with scripted vectors for testing
// the API embedder's dimension discovery.
type fakeLLMEmbedder struct {
	dim int
}

func (f *fakeLLMEmbedder) CreateEmbeddings(_ context.Context, texts []string, _ string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = make([]float32, f.dim)
		out[i][0] = 1
	}
	return out, nil
}

func TestAPIEmbedderDimDiscovery(t *testing.T) {
	a := NewAPIEmbedder(&fakeLLMEmbedder{dim: 64}, "text-embedding-test", 0)
	vecs, err := a.Embed(context.Background(), []string{"x", "y"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 2 || len(vecs[0]) != 64 {
		t.Fatalf("got %d vectors of dim %d, want 2 of dim 64", len(vecs), len(vecs[0]))
	}
	if a.Dim() != 64 {
		t.Fatalf("Dim = %d, want 64", a.Dim())
	}
}

func TestAPIEmbedderConfigDimOverrides(t *testing.T) {
	a := NewAPIEmbedder(&fakeLLMEmbedder{dim: 64}, "m", 128)
	if a.Dim() != 128 {
		t.Fatalf("Dim = %d, want configured 128", a.Dim())
	}
}

func TestWordPieceTokenizerBasic(t *testing.T) {
	// Build a tiny vocab in memory via a temp file.
	vocab := strings.Join([]string{
		"[PAD]", "[UNK]", "[CLS]", "[SEP]",
		"hello", "world", "foo", "bar", "##ing", "test", "run", "ing",
	}, "\n")
	tmp := t.TempDir() + "/vocab.txt"
	if err := writeFile(tmp, vocab); err != nil {
		t.Fatalf("write vocab: %v", err)
	}
	tok, err := loadWordPieceTokenizer(tmp, 16, true)
	if err != nil {
		t.Fatalf("load tokenizer: %v", err)
	}
	in, err := tok.encode([]string{"hello world", "foo"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// Each sequence is [CLS] ... [SEP]; longest is "hello world" = 4 tokens.
	if len(in.inputIDs) != 2 {
		t.Fatalf("want 2 sequences, got %d", len(in.inputIDs))
	}
	if in.seqLen != 4 {
		t.Fatalf("seqLen = %d, want 4", in.seqLen)
	}
	// First token of first sequence must be [CLS].
	if in.inputIDs[0][0] != int64(tok.clsID) {
		t.Fatalf("first token = %d, want cls %d", in.inputIDs[0][0], tok.clsID)
	}
	// Last non-pad token of first sequence must be [SEP].
	if in.inputIDs[0][3] != int64(tok.sepID) {
		t.Fatalf("last token = %d, want sep %d", in.inputIDs[0][3], tok.sepID)
	}
	// The shorter sequence "foo" => [CLS] foo [SEP] = 3 real tokens, padded
	// at seqLen 4. The pad position (index 3) must have attention mask 0.
	if in.attentionMask[1][3] != 0 {
		t.Fatalf("pad position attention should be 0, got %d", in.attentionMask[1][3])
	}
	// And the real [SEP] of the shorter sequence (index 2) must be masked in.
	if in.attentionMask[1][2] != 1 {
		t.Fatalf("sep attention should be 1, got %d", in.attentionMask[1][2])
	}
}

func TestWordPieceSubword(t *testing.T) {
	vocab := strings.Join([]string{
		"[PAD]", "[UNK]", "[CLS]", "[SEP]",
		"running", "run", "##ning",
	}, "\n")
	tmp := t.TempDir() + "/vocab.txt"
	if err := writeFile(tmp, vocab); err != nil {
		t.Fatalf("write vocab: %v", err)
	}
	tok, err := loadWordPieceTokenizer(tmp, 16, true)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// "running" is a whole vocab entry -> single token.
	if toks := tok.tokenize("running"); len(toks) != 1 || toks[0] != tok.vocab["running"] {
		t.Fatalf("running whole-match failed: %v", toks)
	}
	// "runX" -> "run" + [UNK] for "X".
	_ = tok.tokenize("runX")
}

// helpers

func vectorsEqual(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func l2norm(v []float32) float64 {
	var s float64
	for _, x := range v {
		s += float64(x) * float64(x)
	}
	if s == 0 {
		return 0
	}
	// sqrt via math without importing math here
	x := s
	for i := 0; i < 40; i++ {
		x = 0.5 * (x + s/x)
	}
	return x
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
