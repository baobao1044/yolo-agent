package corerag

import (
	"math"
)

// RelevanceScorer computes S(v) = alpha*Semantic + beta*EdgeWeight +
// gamma*Centrality - delta*UtilityPenalty, clamped to [0,1]. Per CORE doc
// §4.1, the threshold theta is applied at expansion time (graph.Expand), not
// here.
type RelevanceScorer struct {
	cfg RelevanceConfig
}

// NewRelevanceScorer builds a scorer from the relevance config.
func NewRelevanceScorer(cfg RelevanceConfig) *RelevanceScorer {
	return &RelevanceScorer{cfg: cfg}
}

// ScoreInput holds the per-node factors needed to compute S(v).
type ScoreInput struct {
	Semantic     float64 // cosine(queryEmb, node.Emb) in [0,1]
	EdgeWeight   float64 // framework-aware edge weight (1.5/1.0/0.5)
	Centrality   float64 // normalized out-degree in [0,1]
	UtilityPenalty float64 // ln(1 + in-degree), unbounded
}

// Score returns the multi-factor relevance, clamped to [0,1].
func (r *RelevanceScorer) Score(in ScoreInput) float64 {
	s := r.cfg.Alpha*clamp01(in.Semantic) +
		r.cfg.Beta*clamp01(in.EdgeWeight) +
		r.cfg.Gamma*clamp01(in.Centrality) -
		r.cfg.Delta*in.UtilityPenalty
	return clamp01(s)
}

// EdgeWeight returns the framework-aware weight for an edge. Call/import edges
// default to 1.0; framework routing edges (Route->Controller, Component->Hook)
// could be elevated to 1.5; name-only matches to 0.5. For the MVP, edges
// parsed from code all carry weight 1.0, so this returns the edge's stored
// weight normalized to [0,1] relative to the 1.5 ceiling.
func EdgeWeight(e Edge) float64 {
	if e.Weight >= 1.5 {
		return 1.0
	}
	if e.Weight >= 1.0 {
		return 1.0 / 1.5
	}
	if e.Weight >= 0.5 {
		return 0.5 / 1.5
	}
	return 0.0
}

// Centrality returns out-degree normalized to [0,1] against the max out-degree
// in the symbol set.
func Centrality(outDegree, maxOutDegree int) float64 {
	if maxOutDegree <= 0 {
		return 0
	}
	return float64(outDegree) / float64(maxOutDegree)
}

// UtilityPenalty returns ln(1 + in-degree), matching CORE doc §4.1.
func UtilityPenalty(inDegree int) float64 {
	return math.Log(1 + float64(inDegree))
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

// Semantic computes cosine similarity between a query embedding and a node's
// embedding, returning a value clamped to [0,1] (cosine in [-1,1]; we treat
// negative similarity as 0 relevance).
func Semantic(query, node []float32) float64 {
	c := cosine(query, node)
	if c < 0 {
		return 0
	}
	return c
}
