package corerag

import (
	"fmt"

	"github.com/baobg/yolo-agent/internal/embeddings"
)

// CoreRAGConfig configures the CORE engine. It is mounted on the top-level
// agent Config as `corerag` and carries the compression priors, relevance
// weights, and budget that govern context assembly.
type CoreRAGConfig struct {
	Enabled   bool                       `yaml:"enabled"`
	Embedding embeddings.EmbeddingConfig `yaml:"embedding"`
	Relevance RelevanceConfig            `yaml:"relevance"`
	Budget    int                        `yaml:"budget"` // token budget B (default 4096)
	Depth     int                        `yaml:"depth"`  // BFS expansion depth k (default 2)
	Priors    map[string]map[int]float64 `yaml:"priors"` // optional override of R(k,Type)
}

// RelevanceConfig holds the weights for the multi-factor relevance score
// S(v) = alpha*Semantic + beta*EdgeWeight + gamma*Centrality - delta*UtilityPenalty.
// Weights should be non-negative and ideally sum to ~1, but this is not
// enforced — the score is clamped to [0,1].
type RelevanceConfig struct {
	Alpha     float64 `yaml:"alpha"`      // semantic similarity weight
	Beta      float64 `yaml:"beta"`       // edge weight
	Gamma     float64 `yaml:"gamma"`       // centrality (out-degree) weight
	Delta     float64 `yaml:"delta"`       // utility penalty (in-degree) weight
	Theta     float64 `yaml:"theta"`       // S>=theta required to expand a node
}

// DefaultCoreRAG returns a config with the defaults from the CORE doc:
// Table 1 compression priors, the doc's relevance formula weights, and a
// 4096-token budget at BFS depth 2. The embedding default is ONNX with API
// fallback so the engine runs even when no local model is installed.
func DefaultCoreRAG() CoreRAGConfig {
	return CoreRAGConfig{
		Enabled: true,
		Embedding: embeddings.EmbeddingConfig{
			Provider:  embeddings.ProviderONNX,
			Fallback:  embeddings.ProviderAPI,
			Model:     "bge-small-en-v1.5",
			Dim:       384,
			OllamaURL: "http://localhost:11434",
		},
		Relevance: RelevanceConfig{
			Alpha: 0.4,
			Beta:  0.2,
			Gamma: 0.2,
			Delta: 0.2,
			Theta: 0.15,
		},
		Budget: 4096,
		Depth:  2,
		Priors: DefaultPriors(),
	}
}

// DefaultPriors returns the empirical compression prior R(k, Type) from CORE
// doc Table 1. R(k, Type) in [0,1] is the average fraction of reasoning-relevant
// information retained when a node of the given type is represented at level k
// instead of raw source (k=0). These defaults may be overridden per-node-type
// via the `priors` config key.
func DefaultPriors() map[string]map[int]float64 {
	return map[string]map[int]float64{
		"Controller": {0: 1.00, 1: 0.95, 2: 0.80, 3: 0.40},
		"DTO":        {0: 1.00, 1: 0.92, 2: 0.75, 3: 0.35},
		"Service":    {0: 1.00, 1: 0.88, 2: 0.65, 3: 0.30},
		"Utility":    {0: 1.00, 1: 0.70, 2: 0.45, 3: 0.20},
		"Other":      {0: 1.00, 1: 0.75, 2: 0.50, 3: 0.25},
	}
}

// PriorTable wraps a resolved prior map with a safe accessor that falls back to
// the "Other" row and to R(k)=1 for unknown levels (never over-compress an
// unknown configuration).
type PriorTable struct {
	r map[string]map[int]float64
}

// NewPriorTable builds a prior table, seeded with defaults and overridden by
// the user-supplied priors (per type and per level).
func NewPriorTable(overrides map[string]map[int]float64) *PriorTable {
	r := DefaultPriors()
	for typ, levels := range overrides {
		if _, ok := r[typ]; !ok {
			r[typ] = map[int]float64{0: 1.0, 1: 0.75, 2: 0.5, 3: 0.25}
		}
		for k, v := range levels {
			r[typ][k] = v
		}
	}
	return &PriorTable{r: r}
}

// R returns the compression prior for the given type and level, falling back to
// the "Other" row, then to 1.0 for level 0 and a conservative 0.5 otherwise.
func (p *PriorTable) R(nodeType NodeType, k Level) float64 {
	row, ok := p.r[string(nodeType)]
	if !ok {
		row = p.r["Other"]
	}
	if row == nil {
		if k == LevelRaw {
			return 1.0
		}
		return 0.5
	}
	if v, ok := row[int(k)]; ok {
		return v
	}
	if k == LevelRaw {
		return 1.0
	}
	return row[int(LevelStub)]
}

// Validate checks that relevance weights are non-negative and the budget and
// depth are positive.
func (c *CoreRAGConfig) Validate() error {
	if c.Budget <= 0 {
		return fmt.Errorf("corerag.budget must be positive, got %d", c.Budget)
	}
	if c.Depth < 0 {
		return fmt.Errorf("corerag.depth must be >= 0, got %d", c.Depth)
	}
	r := c.Relevance
	if r.Alpha < 0 || r.Beta < 0 || r.Gamma < 0 || r.Delta < 0 {
		return fmt.Errorf("corerag.relevance weights must be non-negative")
	}
	if r.Theta < 0 || r.Theta > 1 {
		return fmt.Errorf("corerag.relevance.theta must be in [0,1], got %v", r.Theta)
	}
	return nil
}
