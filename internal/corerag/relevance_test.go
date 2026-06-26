package corerag

import "testing"

func TestRelevanceScoreBounds(t *testing.T) {
	s := NewRelevanceScorer(RelevanceConfig{Alpha: 0.4, Beta: 0.2, Gamma: 0.2, Delta: 0.2})
	// All factors high, no penalty.
	if v := s.Score(ScoreInput{Semantic: 1.0, EdgeWeight: 1.0, Centrality: 1.0, UtilityPenalty: 0}); v > 1.0 || v < 0 {
		t.Fatalf("score out of [0,1]: %v", v)
	}
	// Penalty drives to 0.
	if v := s.Score(ScoreInput{Semantic: 0, EdgeWeight: 0, Centrality: 0, UtilityPenalty: 5}); v != 0 {
		t.Fatalf("expected clamp to 0, got %v", v)
	}
	// Perfect positive factors.
	if v := s.Score(ScoreInput{Semantic: 1.0, EdgeWeight: 1.0, Centrality: 1.0, UtilityPenalty: 0}); v != 0.8 {
		t.Fatalf("expected 0.4+0.2+0.2=0.8, got %v", v)
	}
}

func TestSemanticNegativeClamped(t *testing.T) {
	// Orthogonal-ish vectors still in [-1,1]; negative similarity => 0.
	q := []float32{1, 0}
	node := []float32{-1, 0} // cosine = -1
	if v := Semantic(q, node); v != 0 {
		t.Fatalf("expected negative cosine clamped to 0, got %v", v)
	}
	pos := []float32{1, 0}
	if v := Semantic(q, pos); v != 1 {
		t.Fatalf("expected cosine 1, got %v", v)
	}
}

func TestUtilityPenaltyAndCentrality(t *testing.T) {
	if v := UtilityPenalty(0); v != 0 {
		t.Fatalf("ln(1+0)=0, got %v", v)
	}
	if v := UtilityPenalty(1); v < 0.69 || v > 0.70 {
		t.Fatalf("ln(2)~=0.693, got %v", v)
	}
	if v := Centrality(5, 10); v != 0.5 {
		t.Fatalf("expected 0.5, got %v", v)
	}
	if v := Centrality(5, 0); v != 0 {
		t.Fatalf("expected 0 when max=0, got %v", v)
	}
}
