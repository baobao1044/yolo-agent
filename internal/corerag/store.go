package corerag

import (
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"
)

// Store persists CORE index data (repositories, symbols, edges) in SQLite.
// It shares the connection opened by the memory store (see cmd/agent/main.go:
// corerag.NewStore(store.DB())) rather than opening its own, matching the
// background store convention.
type Store struct {
	db *sql.DB
}

// NewStore creates a corerag store on the given shared connection and runs
// migrations. A nil db is rejected, mirroring background.NewStore.
func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("corerag store: db is required")
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("corerag migrate: %w", err)
	}
	return s, nil
}

// migrate creates the corerag tables if they do not exist.
func (s *Store) migrate() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS corerag_repos (
			id TEXT PRIMARY KEY,
			path TEXT NOT NULL,
			hash TEXT NOT NULL,
			lang TEXT NOT NULL,
			created_at TEXT NOT NULL,
			indexed_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS corerag_symbols (
			id TEXT PRIMARY KEY,
			repo_id TEXT NOT NULL,
			path TEXT NOT NULL,
			name TEXT NOT NULL,
			kind TEXT NOT NULL,
			node_type TEXT NOT NULL,
			signature TEXT,
			doc TEXT,
			body TEXT,
			byte_start INTEGER,
			byte_end INTEGER,
			embedding TEXT,
			in_degree INTEGER DEFAULT 0,
			out_degree INTEGER DEFAULT 0,
			FOREIGN KEY (repo_id) REFERENCES corerag_repos(id)
		)`,
		`CREATE TABLE IF NOT EXISTS corerag_edges (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repo_id TEXT NOT NULL,
			src TEXT NOT NULL,
			dst TEXT NOT NULL,
			kind TEXT NOT NULL,
			weight REAL DEFAULT 1.0,
			FOREIGN KEY (repo_id) REFERENCES corerag_repos(id)
		)`,
		// Indexes for the query path: lookup symbols by repo, edges by endpoint.
		`CREATE INDEX IF NOT EXISTS idx_corerag_symbols_repo ON corerag_symbols(repo_id)`,
		`CREATE INDEX IF NOT EXISTS idx_corerag_edges_src ON corerag_edges(src)`,
		`CREATE INDEX IF NOT EXISTS idx_corerag_edges_dst ON corerag_edges(dst)`,
	}
	for _, q := range queries {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("exec migration: %w", err)
		}
	}
	return nil
}

// SaveRepo upserts a repository record.
func (s *Store) SaveRepo(r RepoMeta) error {
	_, err := s.db.Exec(`INSERT INTO corerag_repos (id, path, hash, lang, created_at, indexed_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET path=excluded.path, hash=excluded.hash,
			lang=excluded.lang, indexed_at=excluded.indexed_at`,
		r.ID, r.Path, r.Hash, r.Lang, r.CreatedAt, r.IndexedAt)
	if err != nil {
		return fmt.Errorf("save repo: %w", err)
	}
	return nil
}

// GetRepo fetches a repository by id.
func (s *Store) GetRepo(id string) (*RepoMeta, error) {
	row := s.db.QueryRow(`SELECT id, path, hash, lang, created_at, indexed_at
		FROM corerag_repos WHERE id = ?`, id)
	r := &RepoMeta{}
	if err := row.Scan(&r.ID, &r.Path, &r.Hash, &r.Lang, &r.CreatedAt, &r.IndexedAt); err != nil {
		return nil, fmt.Errorf("get repo: %w", err)
	}
	return r, nil
}

// FindRepoByPath returns the most recently indexed repo at the given path, if any.
func (s *Store) FindRepoByPath(path string) (*RepoMeta, error) {
	row := s.db.QueryRow(`SELECT id, path, hash, lang, created_at, indexed_at
		FROM corerag_repos WHERE path = ? ORDER BY indexed_at DESC LIMIT 1`, path)
	r := &RepoMeta{}
	if err := row.Scan(&r.ID, &r.Path, &r.Hash, &r.Lang, &r.CreatedAt, &r.IndexedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("find repo: %w", err)
	}
	return r, nil
}

// ListRepos returns all indexed repositories.
func (s *Store) ListRepos() ([]RepoMeta, error) {
	rows, err := s.db.Query(`SELECT id, path, hash, lang, created_at, indexed_at
		FROM corerag_repos ORDER BY indexed_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}
	defer rows.Close()
	var out []RepoMeta
	for rows.Next() {
		var r RepoMeta
		if err := rows.Scan(&r.ID, &r.Path, &r.Hash, &r.Lang, &r.CreatedAt, &r.IndexedAt); err != nil {
			return nil, fmt.Errorf("scan repo: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteRepo removes a repo and all its symbols/edges.
func (s *Store) DeleteRepo(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin delete: %w", err)
	}
	for _, q := range []string{
		`DELETE FROM corerag_edges WHERE repo_id = ?`,
		`DELETE FROM corerag_symbols WHERE repo_id = ?`,
		`DELETE FROM corerag_repos WHERE id = ?`,
	} {
		if _, err := tx.Exec(q, id); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("delete repo data: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete: %w", err)
	}
	return nil
}

// ReplaceRepoSymbols atomically swaps all symbols and edges for a repo.
func (s *Store) ReplaceRepoSymbols(repoID string, symbols []Symbol, edges []Edge) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin replace: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM corerag_symbols WHERE repo_id = ?`, repoID); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("clear symbols: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM corerag_edges WHERE repo_id = ?`, repoID); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("clear edges: %w", err)
	}

	symStmt, err := tx.Prepare(`INSERT INTO corerag_symbols
		(id, repo_id, path, name, kind, node_type, signature, doc, body,
		 byte_start, byte_end, embedding, in_degree, out_degree)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("prepare symbol insert: %w", err)
	}
	defer symStmt.Close()

	for _, sy := range symbols {
		emb, _ := json.Marshal(sy.Embedding)
		if _, err := symStmt.Exec(sy.ID, sy.RepoID, sy.Path, sy.Name, sy.Kind,
			string(sy.NodeType), sy.Signature, sy.Doc, sy.Body,
			sy.ByteStart, sy.ByteEnd, string(emb), sy.InDegree, sy.OutDegree); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert symbol %q: %w", sy.ID, err)
		}
	}

	edgeStmt, err := tx.Prepare(`INSERT INTO corerag_edges
		(repo_id, src, dst, kind, weight) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("prepare edge insert: %w", err)
	}
	defer edgeStmt.Close()
	for _, e := range edges {
		if _, err := edgeStmt.Exec(e.RepoID, e.From, e.To, string(e.Kind), e.Weight); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert edge: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit replace: %w", err)
	}
	return nil
}

// CountSymbols returns the number of persisted symbols for a repo. Used on
// the index no-op path to report accurate counts without re-parsing.
func (s *Store) CountSymbols(repoID string) (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM corerag_symbols WHERE repo_id = ?`, repoID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count symbols: %w", err)
	}
	return n, nil
}

// CountEdges returns the number of persisted edges for a repo.
func (s *Store) CountEdges(repoID string) (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM corerag_edges WHERE repo_id = ?`, repoID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count edges: %w", err)
	}
	return n, nil
}

// LoadSymbols returns all symbols for a repo (with embeddings).
func (s *Store) LoadSymbols(repoID string) ([]Symbol, error) {
	rows, err := s.db.Query(`SELECT id, repo_id, path, name, kind, node_type,
		signature, doc, body, byte_start, byte_end, embedding, in_degree, out_degree
		FROM corerag_symbols WHERE repo_id = ?`, repoID)
	if err != nil {
		return nil, fmt.Errorf("load symbols: %w", err)
	}
	defer rows.Close()
	var out []Symbol
	for rows.Next() {
		sy, err := scanSymbol(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sy)
	}
	return out, rows.Err()
}

// scanner abstracts *sql.Row and *sql.Rows so the same scan helper serves both.
type scanner interface {
	Scan(dest ...any) error
}

// scanSymbol reads a symbol row from either a Row or Rows.
func scanSymbol(sc scanner) (Symbol, error) {
	var sy Symbol
	var nodeType, embStr string
	if err := sc.Scan(&sy.ID, &sy.RepoID, &sy.Path, &sy.Name, &sy.Kind, &nodeType,
		&sy.Signature, &sy.Doc, &sy.Body, &sy.ByteStart, &sy.ByteEnd,
		&embStr, &sy.InDegree, &sy.OutDegree); err != nil {
		return Symbol{}, fmt.Errorf("scan symbol: %w", err)
	}
	sy.NodeType = NodeType(nodeType)
	if embStr != "" {
		_ = json.Unmarshal([]byte(embStr), &sy.Embedding)
	}
	return sy, nil
}

// LoadEdges returns all edges for a repo.
func (s *Store) LoadEdges(repoID string) ([]Edge, error) {
	rows, err := s.db.Query(`SELECT id, repo_id, src, dst, kind, weight
		FROM corerag_edges WHERE repo_id = ?`, repoID)
	if err != nil {
		return nil, fmt.Errorf("load edges: %w", err)
	}
	defer rows.Close()
	var out []Edge
	for rows.Next() {
		var e Edge
		var kind string
		if err := rows.Scan(&e.ID, &e.RepoID, &e.From, &e.To, &kind, &e.Weight); err != nil {
			return nil, fmt.Errorf("scan edge: %w", err)
		}
		e.Kind = EdgeKind(kind)
		out = append(out, e)
	}
	return out, rows.Err()
}
