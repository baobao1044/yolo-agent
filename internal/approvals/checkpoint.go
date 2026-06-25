package approvals

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// RiskLevel describes how dangerous an action might be.
type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

// ActionRequest represents a proposed action that may need approval.
type ActionRequest struct {
	ID          string          `json:"id"`
	ToolName    string          `json:"tool_name"`
	Arguments   json.RawMessage `json:"arguments"`
	Risk        RiskLevel       `json:"risk"`
	Reason      string          `json:"reason"`
	Proposed    string          `json:"proposed"`
	RequestedAt time.Time       `json:"requested_at"`
	Approved    *bool           `json:"approved,omitempty"`
	ResolvedAt  *time.Time      `json:"resolved_at,omitempty"`
}

// Checkpoint handles human-in-the-loop approvals.
type Checkpoint struct {
	policy     *Policy
	store      ApprovalStore
	uiCallback func(*ActionRequest) (*bool, error)
}

// ApprovalStore persists approval requests.
type ApprovalStore interface {
	SaveRequest(ctx context.Context, req *ActionRequest) error
	GetRequest(ctx context.Context, id string) (*ActionRequest, error)
	UpdateRequest(ctx context.Context, req *ActionRequest) error
	ListPending(ctx context.Context) ([]*ActionRequest, error)
}

// NewCheckpoint creates a checkpoint handler.
func NewCheckpoint(policy *Policy, store ApprovalStore, uiCallback func(*ActionRequest) (*bool, error)) *Checkpoint {
	return &Checkpoint{
		policy:     policy,
		store:      store,
		uiCallback: uiCallback,
	}
}

// Evaluate checks whether a tool call requires approval and blocks until resolved.
func (c *Checkpoint) Evaluate(ctx context.Context, toolName string, args json.RawMessage) (bool, error) {
	risk := c.policy.Assess(toolName, args)
	if risk == RiskLow {
		return true, nil
	}

	req := &ActionRequest{
		ID:          generateID(),
		ToolName:    toolName,
		Arguments:   args,
		Risk:        risk,
		Reason:      describeRisk(toolName, args, risk),
		Proposed:    fmt.Sprintf("%s %s", toolName, string(args)),
		RequestedAt: time.Now(),
	}

	if err := c.store.SaveRequest(ctx, req); err != nil {
		return false, fmt.Errorf("save approval request: %w", err)
	}

	// If no UI callback, auto-reject high/medium risk actions
	if c.uiCallback == nil {
		return false, fmt.Errorf("approval required for %s but no UI handler configured; configure auto-approve for non-interactive mode", toolName)
	}

	approved, err := c.uiCallback(req)
	if err != nil {
		return false, fmt.Errorf("approval UI error: %w", err)
	}
	if approved == nil {
		return false, fmt.Errorf("approval request %s not resolved", req.ID)
	}

	now := time.Now()
	req.Approved = approved
	req.ResolvedAt = &now
	_ = c.store.UpdateRequest(ctx, req)

	return *approved, nil
}

// Policy defines rules for assessing risk of tool calls.
type Policy struct {
	AutoApproveLowRisk   bool     `json:"auto_approve_low_risk" yaml:"auto_approve_low_risk"`
	RequireApprovalFor   []string `json:"require_approval_for" yaml:"require_approval_for"`
	DenyPatternStrings []string `json:"deny_patterns" yaml:"deny_patterns"`
	highRiskPatterns     []*regexp.Regexp
}

// DefaultPolicy returns a sensible default approval policy.
func DefaultPolicy() *Policy {
	p := &Policy{
		AutoApproveLowRisk: true,
		RequireApprovalFor: []string{"terminal", "computer", "mcp_"},
		DenyPatternStrings: []string{
			`(?i)rm\s+-rf\s*/`,
			`(?i)mkfs`,
			`(?i)dd\s+if=.*of=/dev/`,
			`(?i)format\s*[C-Z]:`,
			`(?i)reg\s+delete\s+HKLM`,
		},
	}
	_ = p.Build()
	return p
}

// Build compiles high-risk regex patterns from DenyPatternStrings.
func (p *Policy) Build() error {
	p.highRiskPatterns = nil
	for _, s := range p.DenyPatternStrings {
		re, err := regexp.Compile(s)
		if err != nil {
			return fmt.Errorf("invalid deny pattern %q: %w", s, err)
		}
		p.highRiskPatterns = append(p.highRiskPatterns, re)
	}
	return nil
}

// Assess evaluates risk for a tool call.
func (p *Policy) Assess(toolName string, args json.RawMessage) RiskLevel {
	// Check explicit deny patterns against arguments string
	argsStr := strings.ToLower(string(args))
	for _, pat := range p.highRiskPatterns {
		if pat.MatchString(argsStr) {
			return RiskHigh
		}
	}

	for _, name := range p.RequireApprovalFor {
		if name == "mcp_" && strings.HasPrefix(toolName, "mcp_") {
			return RiskMedium
		}
		if toolName == name || strings.HasPrefix(toolName, name+"_") {
			return RiskMedium
		}
	}

	return RiskLow
}

// describeRisk explains why risk was assigned.
func describeRisk(toolName string, args json.RawMessage, risk RiskLevel) string {
	return fmt.Sprintf("Tool %s with args %s assessed as %s risk", toolName, string(args), risk)
}

// generateID creates a simple unique ID for an approval request.
func generateID() string {
	return fmt.Sprintf("aprv-%d", time.Now().UnixNano())
}
