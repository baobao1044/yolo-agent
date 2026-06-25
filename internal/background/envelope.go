package background

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Status represents the lifecycle state of a background task.
type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusPaused    Status = "paused"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusKilled    Status = "killed"
)

// Envelope defines the permission and resource bounds for a background task.
type Envelope struct {
	ID          string            `json:"id" yaml:"id"`
	Name        string            `json:"name" yaml:"name"`
	Description string            `json:"description" yaml:"description"`
	Instruction string            `json:"instruction" yaml:"instruction"`
	Schedule    string            `json:"schedule" yaml:"schedule"` // cron expression or "@once", "@interval 5m"
	Scope       Scope             `json:"scope" yaml:"scope"`
	Budget      Budget            `json:"budget" yaml:"budget"`
	Notify      NotifyConfig      `json:"notify" yaml:"notify"`
	Status      Status            `json:"status" yaml:"status"`
	CreatedAt   time.Time         `json:"created_at" yaml:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at" yaml:"updated_at"`
	LastRunAt   *time.Time        `json:"last_run_at,omitempty" yaml:"last_run_at,omitempty"`
	NextRunAt   *time.Time        `json:"next_run_at,omitempty" yaml:"next_run_at,omitempty"`
	RunCount    int               `json:"run_count" yaml:"run_count"`
	TotalCalls  int               `json:"total_calls" yaml:"total_calls"`
	TotalTokens int               `json:"total_tokens" yaml:"total_tokens"`
	Errors      []string          `json:"errors" yaml:"errors"`
	mu          sync.RWMutex
}

// Scope defines what a background task is allowed to do.
type Scope struct {
	AllowedTools []string `json:"allowed_tools" yaml:"allowed_tools"`
	DeniedTools  []string `json:"denied_tools" yaml:"denied_tools"`
	AllowAll     bool     `json:"allow_all" yaml:"allow_all"`
}

// Budget defines resource caps for a background task.
type Budget struct {
	MaxTokens   int `json:"max_tokens" yaml:"max_tokens"`     // 0 = unlimited
	MaxCalls    int `json:"max_calls" yaml:"max_calls"`       // 0 = unlimited
	MaxRuns     int `json:"max_runs" yaml:"max_runs"`         // 0 = unlimited
	MaxDuration int `json:"max_duration" yaml:"max_duration"` // seconds, 0 = unlimited
}

// NotifyConfig defines when and how the agent reports back.
type NotifyConfig struct {
	OnStart   bool     `json:"on_start" yaml:"on_start"`
	OnFinish  bool     `json:"on_finish" yaml:"on_finish"`
	OnBlock   bool     `json:"on_block" yaml:"on_block"`
	OnError   bool     `json:"on_error" yaml:"on_error"`
	OnBudget  bool     `json:"on_budget" yaml:"on_budget"`
	Channels  []string `json:"channels" yaml:"channels"` // e.g. "tui", "telegram", "discord", "slack", "email"
}

// DefaultEnvelope returns a safe default envelope.
func DefaultEnvelope() *Envelope {
	return &Envelope{
		ID:        generateID(),
		Status:    StatusPending,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		Scope: Scope{
			AllowedTools: []string{"respond", "terminal", "browser"},
			DeniedTools:    []string{"computer"},
		},
		Budget: Budget{
			MaxTokens:   100000,
			MaxCalls:    100,
			MaxRuns:     0,
			MaxDuration: 300,
		},
		Notify: NotifyConfig{
			OnFinish: true,
			OnBlock:  true,
			OnError:  true,
			OnBudget: true,
			Channels: []string{"tui"},
		},
		Errors: make([]string, 0),
	}
}

// IsToolAllowed reports whether a tool name is permitted by the envelope scope.
func (e *Envelope) IsToolAllowed(name string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.Scope.AllowAll {
		return true
	}

	for _, denied := range e.Scope.DeniedTools {
		if denied == name || denied == "*" {
			return false
		}
	}

	if len(e.Scope.AllowedTools) == 0 {
		return false
	}

	for _, allowed := range e.Scope.AllowedTools {
		if allowed == name || allowed == "*" {
			return true
		}
	}
	return false
}

// CanRun checks if the envelope has remaining budget to execute another run.
func (e *Envelope) CanRun() error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.Status == StatusPaused || e.Status == StatusKilled {
		return fmt.Errorf("envelope %s is %s", e.ID, e.Status)
	}

	if e.Budget.MaxRuns > 0 && e.RunCount >= e.Budget.MaxRuns {
		return fmt.Errorf("max runs exceeded: %d/%d", e.RunCount, e.Budget.MaxRuns)
	}

	return nil
}

// CheckBudget returns an error if a proposed tool call would exceed limits.
func (e *Envelope) CheckBudget(additionalTokens, additionalCalls int) error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.Budget.MaxTokens > 0 && e.TotalTokens+additionalTokens > e.Budget.MaxTokens {
		return fmt.Errorf("token budget would be exceeded: %d + %d > %d", e.TotalTokens, additionalTokens, e.Budget.MaxTokens)
	}

	if e.Budget.MaxCalls > 0 && e.TotalCalls+additionalCalls > e.Budget.MaxCalls {
		return fmt.Errorf("call budget would be exceeded: %d + %d > %d", e.TotalCalls, additionalCalls, e.Budget.MaxCalls)
	}

	return nil
}

// RecordUsage updates consumed tokens and calls atomically.
func (e *Envelope) RecordUsage(tokens, calls int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.TotalTokens += tokens
	e.TotalCalls += calls
}

// RecordError appends an error message (bounded).
func (e *Envelope) RecordError(msg string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.Errors) >= 50 {
		e.Errors = e.Errors[1:]
	}
	e.Errors = append(e.Errors, msg)
}

// SetStatus transitions the envelope status and updates the timestamp.
func (e *Envelope) SetStatus(s Status) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Status = s
	e.UpdatedAt = time.Now().UTC()
}

// Snapshot returns a copy safe for JSON serialization.
func (e *Envelope) Snapshot() Envelope {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return *e
}

// Validate checks the envelope for sanity.
func (e *Envelope) Validate() error {
	if e.Instruction == "" {
		return fmt.Errorf("instruction is required")
	}
	if e.Schedule == "" {
		return fmt.Errorf("schedule is required")
	}
	if !e.Scope.AllowAll && len(e.Scope.AllowedTools) == 0 {
		return fmt.Errorf("scope must allow at least one tool")
	}
	return nil
}

// generateID creates a short unique identifier.
func generateID() string {
	return fmt.Sprintf("env_%d", time.Now().UnixNano())
}

// Notifier emits status updates to configured channels.
type Notifier interface {
	Notify(ctx context.Context, envelopeID, title, message string, channels []string) error
}

// Compile-time check for default envelope values.
var _ = DefaultEnvelope()
