package embeddings

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"unicode"
)

// wordPieceTokenizer is a simplified BERT-style WordPiece tokenizer sufficient
// for BGE/E5 sentence-embedding models (which are BERT-based). It is NOT a
// drop-in HuggingFace replacement; it omits some edge-case punctuation rules
// and CJK handling, but produces correct tokens for the common case of
// code/English text. It is pure Go (no cgo) and can be unit-tested in
// isolation.
type wordPieceTokenizer struct {
	vocab      map[string]int // token -> id
	unkID      int
	clsID      int
	sepID      int
	padID      int
	maxLen     int
	doLower    bool
}

const (
	clsToken = "[CLS]"
	sepToken = "[SEP]"
	padToken = "[PAD]"
	unkToken = "[UNK]"
)

// loadWordPieceTokenizer reads a BERT vocab.txt (one token per line) and
// returns a tokenizer configured for the given max sequence length.
func loadWordPieceTokenizer(vocabPath string, maxLen int, doLower bool) (*wordPieceTokenizer, error) {
	f, err := os.Open(vocabPath)
	if err != nil {
		return nil, fmt.Errorf("open vocab: %w", err)
	}
	defer f.Close()

	vocab := make(map[string]int)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	idx := 0
	for sc.Scan() {
		tok := strings.TrimRight(sc.Text(), "\r")
		vocab[tok] = idx
		idx++
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read vocab: %w", err)
	}
	if len(vocab) == 0 {
		return nil, fmt.Errorf("vocab file %q is empty", vocabPath)
	}

	t := &wordPieceTokenizer{
		vocab:   vocab,
		maxLen:  maxLen,
		doLower: doLower,
	}
	t.unkID, _ = vocab[unkToken]
	t.clsID, _ = vocab[clsToken]
	t.sepID, _ = vocab[sepToken]
	t.padID, _ = vocab[padToken]
	return t, nil
}

// encode produces input_ids, token_type_ids, and attention_mask for a batch of
// texts, padding each to the same length (the longest in the batch, capped at
// maxLen). All three returned slices have shape [batch][seqLen].
type bertInputs struct {
	inputIDs       [][]int64
	attentionMask  [][]int64
	tokenTypeIDs   [][]int64
	seqLen         int
}

func (t *wordPieceTokenizer) encode(texts []string) (bertInputs, error) {
	out := bertInputs{}
	if len(texts) == 0 {
		return out, nil
	}

	encoded := make([][]int, len(texts))
	maxSeq := 0
	for i, text := range texts {
		toks := t.tokenize(text)
		// Reserve slots for [CLS] and [SEP]; truncate if needed.
		avail := t.maxLen - 2
		if avail < 1 {
			avail = 1
		}
		if len(toks) > avail {
			toks = toks[:avail]
		}
		ids := make([]int, 0, len(toks)+2)
		ids = append(ids, t.clsID)
		ids = append(ids, toks...)
		ids = append(ids, t.sepID)
		encoded[i] = ids
		if len(ids) > maxSeq {
			maxSeq = len(ids)
		}
	}

	out.seqLen = maxSeq
	out.inputIDs = make([][]int64, len(texts))
	out.attentionMask = make([][]int64, len(texts))
	out.tokenTypeIDs = make([][]int64, len(texts))
	for i, ids := range encoded {
		in := make([]int64, maxSeq)
		mask := make([]int64, maxSeq)
		ty := make([]int64, maxSeq)
		for j, id := range ids {
			in[j] = int64(id)
			mask[j] = 1
		}
		// Padding already zero-valued; token_type_ids all-zero (single segment).
		out.inputIDs[i] = in
		out.attentionMask[i] = mask
		out.tokenTypeIDs[i] = ty
	}
	return out, nil
}

// tokenize performs whitespace + basic punctuation splitting then WordPiece
// subword segmentation against the vocab.
func (t *wordPieceTokenizer) tokenize(text string) []int {
	if t.doLower {
		text = strings.ToLower(text)
	}
	tokens := []string{}
	for _, word := range strings.FieldsFunc(text, func(r rune) bool { return unicode.IsSpace(r) }) {
		// Split off leading/trailing punctuation into separate tokens.
		for _, sub := range splitPunct(word) {
			if sub == "" {
				continue
			}
			toks := t.wordPiece(sub)
			tokens = append(tokens, toks...)
		}
	}

	ids := make([]int, 0, len(tokens))
	for _, tok := range tokens {
		id, ok := t.vocab[tok]
		if !ok {
			id = t.unkID
		}
		ids = append(ids, id)
	}
	return ids
}

// wordPiece segments a single word into subword tokens. The first piece is the
// word itself; continuation pieces are prefixed with "##".
func (t *wordPieceTokenizer) wordPiece(word string) []string {
	if word == "" {
		return nil
	}
	if id, ok := t.vocab[word]; ok && id >= 0 {
		return []string{word}
	}
	// Greedy longest-match from the start.
	var result []string
	start := 0
	for start < len(word) {
		end := len(word)
		found := ""
		for end > start {
			candidate := word[start:end]
			if start > 0 {
				candidate = "##" + candidate
			}
			if _, ok := t.vocab[candidate]; ok {
				found = candidate
				break
			}
			end--
		}
		if found == "" {
			return []string{unkToken}
		}
		result = append(result, found)
		start = end
	}
	return result
}

// splitPunct separates leading and trailing punctuation from a word into their
// own pieces, so that e.g. "foo," -> ["foo", ","]. (Simplified vs. full BERT.)
func splitPunct(word string) []string {
	if word == "" {
		return nil
	}
	parts := []string{}
	start := 0
	for start < len(word) && isPunct(rune(word[start])) {
		parts = append(parts, string(word[start]))
		start++
	}
	end := len(word)
	for end > start && isPunct(rune(word[end-1])) {
		end--
	}
	if end > start {
		parts = append(parts, word[start:end])
	}
	for i := len(word) - 1; i >= end; i-- {
		parts = append(parts, string(word[i]))
	}
	return parts
}

func isPunct(r rune) bool {
	return r == '.' || r == ',' || r == '!' || r == '?' || r == ';' || r == ':' ||
		r == '(' || r == ')' || r == '[' || r == ']' || r == '{' || r == '}' ||
		r == '"' || r == '\'' || r == '`' || r == '/' || r == '\\' || r == '=' ||
		r == '<' || r == '>' || r == '|' || r == '&' || r == '*' || r == '+' ||
		r == '-' || r == '@' || r == '#' || r == '$' || r == '%' || r == '^' ||
		r == '~'
}
