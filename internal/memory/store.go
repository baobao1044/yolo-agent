package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Store manages conversation persistence using SQLite.
type Store struct {
	db *sql.DB
}

// NewStore creates a new memory store.
func NewStore(dbPath string) (*Store, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Enable WAL mode for better concurrent performance
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	store := &Store{db: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return store, nil
}

// migrate creates the database schema.
func (s *Store) migrate() error {
	// Conversations table
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS conversations (
			id TEXT PRIMARY KEY,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			metadata TEXT DEFAULT '{}'
		)
	`)
	if err != nil {
		return fmt.Errorf("create conversations table: %w", err)
	}

	// Messages table
	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			conversation_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT,
			tool_calls TEXT,
			tool_call_id TEXT,
			name TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (conversation_id) REFERENCES conversations(id)
		)
	`)
	if err != nil {
		return fmt.Errorf("create messages table: %w", err)
	}

	// Workflow runs table
	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS workflow_runs (
			id TEXT PRIMARY KEY,
			conversation_id TEXT,
			plan TEXT NOT NULL,
			status TEXT DEFAULT 'running',
			result TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			completed_at DATETIME,
			FOREIGN KEY (conversation_id) REFERENCES conversations(id)
		)
	`)
	if err != nil {
		return fmt.Errorf("create workflow_runs table: %w", err)
	}

	// Tool calls audit log
	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS tool_call_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			conversation_id TEXT,
			tool_name TEXT NOT NULL,
			arguments TEXT,
			result TEXT,
			is_error BOOLEAN DEFAULT FALSE,
			duration_ms INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("create tool_call_log table: %w", err)
	}

	// FTS5 index for full-text search
	_, err = s.db.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
			content,
			content=messages,
			content_rowid=id
		)
	`)
	if err != nil {
		// FTS5 might not be available, log but don't fail
		fmt.Fprintf(os.Stderr, "warning: FTS5 not available: %v\n", err)
	}

	return nil
}

// SaveConversation saves or updates a conversation.
func (s *Store) SaveConversation(id string, metadata map[string]string) error {
	metaJSON, _ := jsonMarshal(metadata)
	_, err := s.db.Exec(`
		INSERT INTO conversations (id, metadata, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET metadata = ?, updated_at = ?
	`, id, metaJSON, time.Now(), metaJSON, time.Now())
	return err
}

// SaveMessage saves a message to a conversation.
func (s *Store) SaveMessage(conversationID, role, content string) (int64, error) {
	result, err := s.db.Exec(`
		INSERT INTO messages (conversation_id, role, content) VALUES (?, ?, ?)
	`, conversationID, role, content)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// LoadMessages loads all messages for a conversation.
func (s *Store) LoadMessages(conversationID string) ([]MessageRecord, error) {
	rows, err := s.db.Query(`
		SELECT id, role, content, tool_calls, tool_call_id, name, created_at
		FROM messages WHERE conversation_id = ? ORDER BY id ASC
	`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []MessageRecord
	for rows.Next() {
		var m MessageRecord
		var toolCalls, toolCallID, name sql.NullString
		if err := rows.Scan(&m.ID, &m.Role, &m.Content, &toolCalls, &toolCallID, &name, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.ToolCalls = toolCalls.String
		m.ToolCallID = toolCallID.String
		m.Name = name.String
		messages = append(messages, m)
	}
	return messages, nil
}

// LogToolCall records a tool call in the audit log.
func (s *Store) LogToolCall(conversationID, toolName, args, result string, isError bool, durationMs int64) error {
	_, err := s.db.Exec(`
		INSERT INTO tool_call_log (conversation_id, tool_name, arguments, result, is_error, duration_ms)
		VALUES (?, ?, ?, ?, ?, ?)
	`, conversationID, toolName, args, result, isError, durationMs)
	return err
}

// SaveWorkflowRun saves a workflow run record.
func (s *Store) SaveWorkflowRun(id, conversationID, plan, status, result string) error {
	now := time.Now()
	completedAt := "NULL"
	if status == "completed" || status == "failed" {
		completedAt = now.Format(time.RFC3339)
	}
	_, err := s.db.Exec(`
		INSERT INTO workflow_runs (id, conversation_id, plan, status, result, completed_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET status = ?, result = ?, completed_at = ?
	`, id, conversationID, plan, status, result, completedAt, status, result, completedAt)
	return err
}

// Search searches messages using FTS5.
func (s *Store) Search(query string, limit int) ([]MessageRecord, error) {
	rows, err := s.db.Query(`
		SELECT m.id, m.role, m.content, m.tool_calls, m.tool_call_id, m.name, m.created_at
		FROM messages_fts f
		JOIN messages m ON m.id = f.rowid
		WHERE messages_fts MATCH ?
		ORDER BY rank
		LIMIT ?
	`, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []MessageRecord
	for rows.Next() {
		var m MessageRecord
		var toolCalls, toolCallID, name sql.NullString
		if err := rows.Scan(&m.ID, &m.Role, &m.Content, &toolCalls, &toolCallID, &name, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.ToolCalls = toolCalls.String
		m.ToolCallID = toolCallID.String
		m.Name = name.String
		messages = append(messages, m)
	}
	return messages, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying database connection (for advanced usage).
func (s *Store) DB() *sql.DB {
	return s.db
}

// MessageRecord is a message as stored in the database.
type MessageRecord struct {
	ID        int64  `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	ToolCalls string `json:"tool_calls,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	CreatedAt string `json:"created_at"`
}

func jsonMarshal(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
