package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	_ "modernc.org/sqlite"
)

// VectorStore manages vector embeddings and semantic memory.
type VectorStore struct {
	db *sql.DB
}

// NewVectorStore creates a vector store backed by SQLite.
// Uses the same Store.db if desired, or a separate db path.
func NewVectorStore(db *sql.DB) (*VectorStore, error) {
	vs := &VectorStore{db: db}
	if err := vs.migrate(); err != nil {
		return nil, err
	}
	return vs, nil
}

// migrate creates memory tables.
func (v *VectorStore) migrate() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS memories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			conversation_id TEXT,
			scope TEXT DEFAULT 'session',
			source TEXT DEFAULT 'chat',
			content TEXT NOT NULL,
			embedding TEXT,
			importance REAL DEFAULT 1.0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
	}

	for _, q := range queries {
		if _, err := v.db.Exec(q); err != nil {
			return fmt.Errorf("memory table migration: %w", err)
		}
	}
	return nil
}

// Memory represents a stored memory.
type Memory struct {
	ID             int64     `json:"id"`
	ConversationID string    `json:"conversation_id"`
	Scope          string    `json:"scope"`
	Source         string    `json:"source"`
	Content        string    `json:"content"`
	Embedding      []float32 `json:"-"`
	Importance     float64   `json:"importance"`
	CreatedAt      time.Time `json:"created_at"`
}

// Store saves a memory with optional embedding.
func (v *VectorStore) Store(ctx context.Context, m Memory) (int64, error) {
	var embJSON []byte
	if len(m.Embedding) > 0 {
		var err error
		embJSON, err = json.Marshal(m.Embedding)
		if err != nil {
			return 0, fmt.Errorf("marshal embedding: %w", err)
		}
	}

	res, err := v.db.ExecContext(ctx, `
		INSERT INTO memories (conversation_id, scope, source, content, embedding, importance)
		VALUES (?, ?, ?, ?, ?, ?)
	`, m.ConversationID, m.Scope, m.Source, m.Content, string(embJSON), m.Importance)
	if err != nil {
		return 0, fmt.Errorf("insert memory: %w", err)
	}
	return res.LastInsertId()
}

// Recall returns the top-k memories most similar to the query embedding.
// If no embeddings are stored, falls back to recency ordering.
func (v *VectorStore) Recall(ctx context.Context, queryEmbedding []float32, topK int) ([]Memory, error) {
	rows, err := v.db.QueryContext(ctx, `
		SELECT id, conversation_id, scope, source, content, embedding, importance, created_at
		FROM memories
		ORDER BY created_at DESC
		LIMIT ?
	`, topK*10)
	if err != nil {
		return nil, fmt.Errorf("query memories: %w", err)
	}
	defer rows.Close()

	var candidates []memoryWithScore
	for rows.Next() {
		var m Memory
		var embString string
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Scope, &m.Source, &m.Content, &embString, &m.Importance, &m.CreatedAt); err != nil {
			return nil, err
		}
		if embString != "" {
			if err := json.Unmarshal([]byte(embString), &m.Embedding); err != nil {
				continue
			}
		}

		score := scoreMemory(queryEmbedding, &m)
		candidates = append(candidates, memoryWithScore{memory: &m, score: score})
	}

	if len(queryEmbedding) > 0 {
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].score > candidates[j].score
		})
	} else {
		// Already ordered by recency; score is hybrid
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].score > candidates[j].score
		})
	}

	if topK > len(candidates) {
		topK = len(candidates)
	}

	result := make([]Memory, 0, topK)
	for i := 0; i < topK; i++ {
		result = append(result, *candidates[i].memory)
	}
	return result, nil
}

// GetByScope returns memories scoped to a user/session.
func (v *VectorStore) GetByScope(ctx context.Context, scope string, limit int) ([]Memory, error) {
	rows, err := v.db.QueryContext(ctx, `
		SELECT id, conversation_id, scope, source, content, embedding, importance, created_at
		FROM memories
		WHERE scope = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, scope, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Memory
	for rows.Next() {
		var m Memory
		var embString string
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Scope, &m.Source, &m.Content, &embString, &m.Importance, &m.CreatedAt); err != nil {
			return nil, err
		}
		if embString != "" {
			_ = json.Unmarshal([]byte(embString), &m.Embedding)
		}
		result = append(result, m)
	}
	return result, nil
}

// Delete removes a memory by ID.
func (v *VectorStore) Delete(ctx context.Context, id int64) error {
	_, err := v.db.ExecContext(ctx, `DELETE FROM memories WHERE id = ?`, id)
	return err
}

// scoreMemory combines similarity, recency, and importance.
func scoreMemory(query []float32, m *Memory) float64 {
	var similarity float64
	if len(query) > 0 && len(m.Embedding) > 0 {
		similarity = float64(cosineSimilarity(query, m.Embedding))
	}

	recency := 1.0
	if !m.CreatedAt.IsZero() {
		hoursAgo := time.Since(m.CreatedAt).Hours()
		recency = math.Exp(-hoursAgo / 168.0) // decay over a week
	}

	// Weighted combination
	return 0.6*similarity + 0.25*recency + 0.15*m.Importance
}

// cosineSimilarity returns cosine similarity between two vectors.
func cosineSimilarity(a, b []float32) float32 {
	var dot, normA, normB float64
	for i := range a {
		if i < len(b) {
			ai := float64(a[i])
			bi := float64(b[i])
			dot += ai * bi
			normA += ai * ai
			normB += bi * bi
		}
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(normA) * math.Sqrt(normB)))
}

type memoryWithScore struct {
	memory *Memory
	score  float64
}
