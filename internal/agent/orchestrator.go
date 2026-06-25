package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/baobg/yolo-agent/internal/tools"
	"github.com/baobg/yolo-agent/internal/workflow"
)

// Orchestrator decides when to spin up multi-agent workflows.
type Orchestrator struct {
	registry *tools.Registry
	logger   *slog.Logger
	config   OrchestratorConfig
}

// OrchestratorConfig configures the orchestrator.
type OrchestratorConfig struct {
	MaxConcurrent int `yaml:"max_concurrent"`
	MaxTotal      int `yaml:"max_total"`
	VerifyRetries int `yaml:"verify_retries"`
}

// NewOrchestrator creates a new orchestrator.
func NewOrchestrator(registry *tools.Registry, config OrchestratorConfig, logger *slog.Logger) *Orchestrator {
	if logger == nil {
		logger = slog.Default()
	}
	if config.MaxConcurrent == 0 {
		config.MaxConcurrent = 16
	}
	if config.MaxTotal == 0 {
		config.MaxTotal = 1000
	}
	if config.VerifyRetries == 0 {
		config.VerifyRetries = 2
	}
	return &Orchestrator{
		registry: registry,
		logger:   logger,
		config:   config,
	}
}

// OrchestrateResult is the result of an orchestration run.
type OrchestrateResult struct {
	Plan      *workflow.OrchestrationPlan `json:"plan"`
	Phases    []PhaseResult               `json:"phases"`
	FinalAnswer string                     `json:"final_answer"`
}

// PhaseResult is the result of a single phase.
type PhaseResult struct {
	Name      string              `json:"name"`
	Tasks     []TaskResult        `json:"tasks"`
	Verify    *VerifyResult       `json:"verify,omitempty"`
}

// TaskResult is the result of a single task within a phase.
type TaskResult struct {
	Prompt  string `json:"prompt"`
	Output  string `json:"output"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// VerifyResult is the result of the adversarial verification phase.
type VerifyResult struct {
	IssuesFound  int    `json:"issues_found"`
	Details      string `json:"details"`
	NeedsRerun   bool   `json:"needs_rerun"`
}

// OrchestrateTool is the tool the LLM calls to trigger multi-agent orchestration.
type OrchestrateTool struct {
	orchestrator *Orchestrator
}

// NewOrchestrateTool creates a new orchestrate tool.
func NewOrchestrateTool(orchestrator *Orchestrator) *OrchestrateTool {
	return &OrchestrateTool{orchestrator: orchestrator}
}

func (t *OrchestrateTool) Name() string {
	return "orchestrate"
}

func (t *OrchestrateTool) Description() string {
	return `Spin up a multi-agent workflow to handle complex tasks. Provide an orchestration plan with phases, tasks, and dependencies. Each task runs as an independent subagent. Phases run sequentially; tasks within a phase run in parallel (up to 16 concurrent). Use "adversarial": true on a phase to add adversarial verification.`
}

func (t *OrchestrateTool) Schema() tools.ToolSchema {
	return tools.ToolSchema{
		Type: "object",
		Properties: map[string]tools.SchemaProperty{
			"plan": {
				Type:        "object",
				Description: `The orchestration plan with phases, tasks, and dependencies. Format: {"phases": [{"name": "understand", "parallel": true, "tasks": [{"prompt": "...", "tools": ["terminal"]}], "depends_on": []}, ...]}`,
			},
		},
		Required: []string{"plan"},
	}
}

func (t *OrchestrateTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Plan json.RawMessage `json:"plan"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("parse orchestrate args: %w", err)
	}

	plan, err := workflow.ParsePlan(params.Plan)
	if err != nil {
		return nil, fmt.Errorf("parse orchestration plan: %w", err)
	}

	// Validate plan
	if err := plan.Validate(); err != nil {
		return nil, fmt.Errorf("invalid orchestration plan: %w", err)
	}

	// Create engine and run
	engine := workflow.NewEngine(t.orchestrator.registry, t.orchestrator.config.MaxConcurrent, t.orchestrator.config.MaxTotal, t.orchestrator.logger)
	result, err := engine.Run(ctx, plan)
	if err != nil {
		return nil, fmt.Errorf("workflow execution failed: %w", err)
	}

	return result, nil
}
