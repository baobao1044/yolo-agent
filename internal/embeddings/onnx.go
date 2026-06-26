//go:build cgo

package embeddings

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// onnxMaxLen is the maximum sequence length passed to the tokenizer.
const onnxMaxLen = 512

// ONNXEmbedder runs a BERT-based sentence embedding model (BGE/E5) in-process
// via onnxruntime. It keeps source code local (no provider round-trip),
// matching the privacy ethos of CORE doc §5. Requires cgo + the onnxruntime
// shared library + a model directory containing the .onnx file and vocab.txt.
type ONNXEmbedder struct {
	session    *ort.DynamicAdvancedSession
	tokenizer  *wordPieceTokenizer
	hiddenSize int
	inputNames []string
	outName    string
	logger     *slog.Logger
}

var (
	ortOnce   sync.Once
	ortInitOk bool
)

// initORT initializes the onnxruntime environment at most once, optionally
// setting the shared library path from config or the YOLO_ONNX_LIB env var.
func initORT(libPath string) error {
	var initErr error
	ortOnce.Do(func() {
		if libPath == "" {
			libPath = os.Getenv("YOLO_ONNX_LIB")
		}
		if libPath != "" {
			ort.SetSharedLibraryPath(libPath)
		}
		if err := ort.InitializeEnvironment(ort.WithLogLevelError()); err != nil {
			initErr = fmt.Errorf("init onnxruntime environment: %w", err)
			return
		}
		ortInitOk = true
	})
	if initErr != nil {
		return initErr
	}
	if !ortInitOk {
		// Already initialized by a prior call; nothing to do.
	}
	return nil
}

// NewONNXEmbedder loads the model and vocab from the configured model directory.
// cfg.ModelPath points at the .onnx file; vocab.txt is expected beside it.
func NewONNXEmbedder(cfg EmbeddingConfig, logger *slog.Logger) (Embedder, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.ModelPath == "" {
		return nil, fmt.Errorf("onnx embedder requires embedding.model_path")
	}
	if _, err := os.Stat(cfg.ModelPath); err != nil {
		return nil, fmt.Errorf("onnx model not found at %q: %w", cfg.ModelPath, err)
	}
	if err := initORT(cfg.LibPath); err != nil {
		return nil, err
	}

	// Discover the model's hidden size from its output shape.
	hidden, outName, err := probeModel(cfg.ModelPath)
	if err != nil {
		return nil, fmt.Errorf("probe onnx model: %w", err)
	}
	if cfg.Dim > 0 && cfg.Dim != hidden {
		// Trust the explicit config but keep going if they diverge.
		logger.Warn("configured embedding dim differs from model output dim", "configured", cfg.Dim, "model", hidden)
	}

	vocabPath := filepath.Join(filepath.Dir(cfg.ModelPath), "vocab.txt")
	tok, err := loadWordPieceTokenizer(vocabPath, onnxMaxLen, true)
	if err != nil {
		return nil, fmt.Errorf("load onnx tokenizer: %w", err)
	}

	inputNames := []string{"input_ids", "attention_mask", "token_type_ids"}
	session, err := ort.NewDynamicAdvancedSession(cfg.ModelPath, inputNames, []string{outName}, nil)
	if err != nil {
		return nil, fmt.Errorf("create onnx session: %w", err)
	}

	return &ONNXEmbedder{
		session:    session,
		tokenizer:  tok,
		hiddenSize: hidden,
		inputNames: inputNames,
		outName:    outName,
		logger:     logger,
	}, nil
}

// probeModel inspects the model's output tensor info to determine the hidden
// size and the output tensor name (commonly "last_hidden_state").
func probeModel(modelPath string) (hidden int, outName string, err error) {
	_, outs, err := ort.GetInputOutputInfo(modelPath)
	if err != nil {
		return 0, "", fmt.Errorf("get output info: %w", err)
	}
	if len(outs) == 0 {
		return 0, "", fmt.Errorf("model has no outputs")
	}
	// Prefer the first output shaped [batch, seq, hidden].
	for _, o := range outs {
		if o.Name != "" {
			outName = o.Name
		}
		sh := shapeDims(o)
		if len(sh) >= 3 {
			hidden = int(sh[len(sh)-1])
		}
	}
	if outName == "" {
		outName = "last_hidden_state"
	}
	if hidden == 0 {
		return 0, "", fmt.Errorf("could not determine hidden size from output shape")
	}
	return hidden, outName, nil
}

// Embed tokenizes the batch, runs the model, mean-pools over the sequence
// dimension (masked), and L2-normalizes the result.
func (e *ONNXEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	inputs, err := e.tokenizer.encode(texts)
	if err != nil {
		return nil, err
	}
	batch := len(texts)
	seq := inputs.seqLen

	// Flatten inputs into contiguous slices for tensor creation.
	inIDs := flatten(inputs.inputIDs)
	attn := flatten(inputs.attentionMask)
	tyIDs := flatten(inputs.tokenTypeIDs)

	inShape := ort.NewShape(int64(batch), int64(seq))
	inTensors := make([]ort.Value, 0, 3)
	idT, err := ort.NewTensor[int64](inShape, inIDs)
	if err != nil {
		return nil, fmt.Errorf("create input_ids tensor: %w", err)
	}
	inTensors = append(inTensors, idT)
	maskT, err := ort.NewTensor[int64](inShape, attn)
	if err != nil {
		idT.Destroy()
		return nil, fmt.Errorf("create attention_mask tensor: %w", err)
	}
	inTensors = append(inTensors, maskT)
	tyT, err := ort.NewTensor[int64](inShape, tyIDs)
	if err != nil {
		idT.Destroy()
		maskT.Destroy()
		return nil, fmt.Errorf("create token_type_ids tensor: %w", err)
	}
	inTensors = append(inTensors, tyT)
	defer func() {
		for _, t := range inTensors {
			_ = t.Destroy()
		}
	}()

	// Output tensor: [batch, seq, hidden].
	outShape := ort.NewShape(int64(batch), int64(seq), int64(e.hiddenSize))
	outData := make([]float32, batch*seq*e.hiddenSize)
	outT, err := ort.NewTensor[float32](outShape, outData)
	if err != nil {
		return nil, fmt.Errorf("create output tensor: %w", err)
	}
	defer outT.Destroy()

	if err := e.session.Run(inTensors, []ort.Value{outT}); err != nil {
		return nil, fmt.Errorf("onnx session run: %w", err)
	}

	// Read back the (possibly in-place updated) output data.
	result := outT.GetData()

	vectors := make([][]float32, batch)
	for b := 0; b < batch; b++ {
		acc := make([]float32, e.hiddenSize)
		var count float32
		for s := 0; s < seq; s++ {
			if attn[b*seq+s] == 0 {
				continue
			}
			off := (b*seq + s) * e.hiddenSize
			for h := 0; h < e.hiddenSize; h++ {
				acc[h] += result[off+h]
			}
			count++
		}
		if count > 0 {
			for h := range acc {
				acc[h] /= count
			}
		}
		normalize(acc)
		vectors[b] = acc
	}
	return vectors, nil
}

// Dim returns the embedding dimensionality.
func (e *ONNXEmbedder) Dim() int { return e.hiddenSize }

// flatten concatenates a [batch][seq] slice into a flat [batch*seq] slice.
func flatten(batch [][]int64) []int64 {
	if len(batch) == 0 {
		return nil
	}
	seq := len(batch[0])
	out := make([]int64, 0, len(batch)*seq)
	for _, row := range batch {
		out = append(out, row...)
	}
	return out
}

// shapeDims extracts the int64 dimensions from an ort.InputOutputInfo. For
// current onnxruntime_go the field is Dimensions (type Shape = []int64).
func shapeDims(o ort.InputOutputInfo) []int64 {
	return o.Dimensions
}
