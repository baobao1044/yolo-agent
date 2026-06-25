// Copyright (c) 2026 Bao Bui Gia / YOLO Agent contributors
// SPDX-License-Identifier: MIT

package background

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Store persists background task envelopes using SQLite.
type Store struct {
	db *sql.DB
}

// NewStore opens the background task store on the given database.
func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("db is required")
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

// migrate creates the required schema.
func (s *Store) migrate() error {
	q := `
CREATE TABLE IF NOT EXISTS background_envelopes (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	instruction TEXT NOT NULL,
	schedule TEXT NOT NULL,
	scope TEXT NOT NULL,
	budget TEXT NOT NULL,
	notify TEXT NOT NULL,
	status TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	last_run_at TEXT,
	next_run_at TEXT,
	run_count INTEGER NOT NULL DEFAULT 0,
	total_calls INTEGER NOT NULL DEFAULT 0,
	total_tokens INTEGER NOT NULL DEFAULT 0,
	errors TEXT
);`
	_, err := s.db.Exec(q)
	return err
}

// Save inserts or replaces an envelope.
func (s *Store) Save(e *Envelope) error {
	scope, err := json.Marshal(e.Scope)
	if err != nil {
		return fmt.Errorf("marshal scope: %w", err)
	}
	budget, err := json.Marshal(e.Budget)
	if err != nil {
		return fmt.Errorf("marshal budget: %w", err)
	}
	notify, err := json.Marshal(e.Notify)
	if err != nil {
		return fmt.Errorf("marshal notify: %w", err)
	}
	errors, err := json.Marshal(e.Errors)
	if err != nil {
		return fmt.Errorf("marshal errors: %w", err)
	}

	q := `
INSERT INTO background_envelopes
	(id, name, description, instruction, schedule, scope, budget, notify, status,
	 created_at, updated_at, last_run_at, next_run_at, run_count, total_calls, total_tokens, errors)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	name=excluded.name,
	description=excluded.description,
	instruction=excluded.instruction,
	schedule=excluded.schedule,
	scope=excluded.scope,
	budget=excluded.budget,
	notify=excluded.notify,
	status=excluded.status,
	updated_at=excluded.updated_at,
	last_run_at=excluded.last_run_at,
	next_run_at=excluded.next_run_at,
	run_count=excluded.run_count,
	total_calls=excluded.total_calls,
	total_tokens=excluded.total_tokens,
	errors=excluded.errors;`

	_, err = s.db.Exec(q,
		e.ID, e.Name, e.Description, e.Instruction, e.Schedule,
		string(scope), string(budget), string(notify), string(e.Status),
		e.CreatedAt.Format(time.RFC3339), e.UpdatedAt.Format(time.RFC3339),
		nullTime(e.LastRunAt), nullTime(e.NextRunAt),
		e.RunCount, e.TotalCalls, e.TotalTokens, string(errors),
	)
	return err
}

// Get retrieves an envelope by ID.
func (s *Store) Get(id string) (*Envelope, error) {
	row := s.db.QueryRow(`
SELECT id, name, description, instruction, schedule, scope, budget, notify, status,
       created_at, updated_at, last_run_at, next_run_at, run_count, total_calls, total_tokens, errors
FROM background_envelopes WHERE id = ?`, id)
	return scanEnvelope(row)
}

// List returns all envelopes ordered by creation time (newest first).
func (s *Store) List() ([]*Envelope, error) {
	rows, err := s.db.Query(`
SELECT id, name, description, instruction, schedule, scope, budget, notify, status,
       created_at, updated_at, last_run_at, next_run_at, run_count, total_calls, total_tokens, errors
FROM background_envelopes
ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Envelope
	for rows.Next() {
		e, err := scanEnvelope(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Delete removes an envelope by ID.
func (s *Store) Delete(id string) error {
	_, err := s.db.Exec(`DELETE FROM background_envelopes WHERE id = ?`, id)
	return err
}

// scanEnvelope reads a row into an Envelope.
func scanEnvelope(scanner interface {
	Scan(dest ...interface{}) error
}) (*Envelope, error) {
	var e Envelope
	var createdAt, updatedAt, lastRunAt, nextRunAt sql.NullString
	var scopeJSON, budgetJSON, notifyJSON, errorsJSON string

	err := scanner.Scan(
		&e.ID, &e.Name, &e.Description, &e.Instruction, &e.Schedule,
		&scopeJSON, &budgetJSON, &notifyJSON, &e.Status,
		&createdAt, &updatedAt, &lastRunAt, &nextRunAt,
		&e.RunCount, &e.TotalCalls, &e.TotalTokens, &errorsJSON,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(scopeJSON), &e.Scope); err != nil {
		return nil, fmt.Errorf("unmarshal scope: %w", err)
	}
	if err := json.Unmarshal([]byte(budgetJSON), &e.Budget); err != nil {
		return nil, fmt.Errorf("unmarshal budget: %w", err)
	}
	if err := json.Unmarshal([]byte(notifyJSON), &e.Notify); err != nil {
		return nil, fmt.Errorf("unmarshal notify: %w", err)
	}
	if err := json.Unmarshal([]byte(errorsJSON), &e.Errors); err != nil {
		e.Errors = []string{}
	}

	e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt.String)
	e.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt.String)
	if lastRunAt.Valid {
		t, _ := time.Parse(time.RFC3339, lastRunAt.String)
		e.LastRunAt = &t
	}
	if nextRunAt.Valid {
		t, _ := time.Parse(time.RFC3339, nextRunAt.String)
		e.NextRunAt = &t
	}

	return &e, nil
}

// nullTime converts a time pointer to a nullable string.
func nullTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.Format(time.RFC3339)
}
