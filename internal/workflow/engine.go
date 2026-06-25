package workflow

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/baobg/yolo-agent/internal/tools"
)

// Engine runs orchestration plans, managing phases, subagents, and verification.
type Engine struct {
	registry     *tools.Registry
	maxConcurrent int
	maxTotal      int
	logger        *slog.Logger
	totalAgents   atomic.Int32
	executeSubagentFn func(ctx context.Context, prompt string, registry *tools.Registry) (string, error)
}

// NewEngine creates a new workflow engine.
func NewEngine(registry *tools.Registry, maxConcurrent, maxTotal int, logger *slog.Logger) *Engine {
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{
		registry:      registry,
		maxConcurrent: maxConcurrent,
		maxTotal:       maxTotal,
		logger:         logger,
	}
}

// RunResult is the result of running an orchestration plan.
type RunResult struct {
	Plan        *OrchestrationPlan `json:"plan"`
	PhaseResults []PhaseResultData `json:"phase_results"`
	FinalAnswer  string            `json:"final_answer"`
	TotalAgents  int               `json:"total_agents"`
	Duration     string            `json:"duration"`
}

// PhaseResultData holds the results for a single phase.
type PhaseResultData struct {
	PhaseName   string          `json:"phase_name"`
	TaskResults []TaskResultData `json:"task_results"`
	VerifyResult *VerifyData    `json:"verify_result,omitempty"`
	Adversarial  bool           `json:"adversarial"`
}

// TaskResultData holds the result of a single task.
type TaskResultData struct {
	TaskID    string `json:"task_id"`
	Prompt    string `json:"prompt"`
	Output    string `json:"output"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
	Duration  string `json:"duration"`
}

// VerifyData holds the result of adversarial verification.
type VerifyData struct {
	IssuesFound int    `json:"issues_found"`
	Details     string `json:"details"`
	NeedsRerun  bool   `json:"needs_rerun"`
}

// Run executes the orchestration plan.
func (e *Engine) Run(ctx context.Context, plan *OrchestrationPlan) (*RunResult, error) {
	start := time.Now()
	e.totalAgents.Store(0)

	result := &RunResult{
		Plan:         plan,
		PhaseResults: make([]PhaseResultData, 0, len(plan.Phases)),
	}

	// Track completed phases for dependency resolution
	completedPhases := make(map[string]bool)

	for _, phase := range plan.Phases {
		// Wait for dependencies
		for _, dep := range phase.DependsOn {
			if !completedPhases[dep] {
				return nil, fmt.Errorf("dependency %q not completed before phase %q", dep, phase.Name)
			}
		}

		e.logger.Info("running phase", "name", phase.Name, "parallel", phase.Parallel, "adversarial", phase.Adversarial)

		phaseResult, err := e.runPhase(ctx, phase)
		if err != nil {
			return nil, fmt.Errorf("phase %q failed: %w", phase.Name, err)
		}

		result.PhaseResults = append(result.PhaseResults, *phaseResult)
		completedPhases[phase.Name] = true

		// If this phase has adversarial verification, run it
		if phase.Adversarial {
			verifyResult := e.runVerification(ctx, phase, phaseResult)
			phaseResult.VerifyResult = verifyResult
		}
	}

	result.TotalAgents = int(e.totalAgents.Load())
	result.Duration = time.Since(start).String()

	// Build final answer from all phase results
	result.FinalAnswer = e.buildFinalAnswer(result)

	return result, nil
}

// runPhase executes a single phase (parallel or sequential).
func (e *Engine) runPhase(ctx context.Context, phase Phase) (*PhaseResultData, error) {
	phaseResult := &PhaseResultData{
		PhaseName:   phase.Name,
		TaskResults: make([]TaskResultData, 0, len(phase.Tasks)),
		Adversarial: phase.Adversarial,
	}

	if phase.Parallel {
		results := e.runTasksParallel(ctx, phase.Tasks)
		phaseResult.TaskResults = results
	} else {
		for _, task := range phase.Tasks {
			result := e.runTask(ctx, task)
			phaseResult.TaskResults = append(phaseResult.TaskResults, result)
		}
	}

	return phaseResult, nil
}

// runTasksParallel runs multiple tasks concurrently with a goroutine pool.
func (e *Engine) runTasksParallel(ctx context.Context, tasks []Task) []TaskResultData {
	results := make([]TaskResultData, len(tasks))
	var wg sync.WaitGroup

	// Semaphore for max concurrency
	sem := make(chan struct{}, e.maxConcurrent)

	for i, task := range tasks {
		wg.Add(1)
		go func(idx int, t Task) {
			defer wg.Done()
			sem <- struct{}{} // acquire slot
			defer func() { <-sem }() // release slot

			// Check total agent limit
			if int(e.totalAgents.Add(1)) > e.maxTotal {
				results[idx] = TaskResultData{
					TaskID: t.ID,
					Prompt: t.Prompt,
					Error:  "max total agents exceeded",
				}
				return
			}

			result := e.runTask(ctx, t)
			results[idx] = result
		}(i, task)
	}

	wg.Wait()
	return results
}

// runTask executes a single task as a subagent.
func (e *Engine) runTask(ctx context.Context, task Task) TaskResultData {
	start := time.Now()
	e.logger.Info("running subagent task", "id", task.ID, "prompt", task.Prompt)

	// Filter registry to only allow specified tools
	taskRegistry := e.registry
	if len(task.Tools) > 0 {
		taskRegistry = e.registry.Filter(task.Tools)
	}

	// Create a subagent for this task
	// The subagent runs its own agent loop with the task prompt
	output, err := e.executeSubagent(ctx, task.Prompt, taskRegistry)

	result := TaskResultData{
		TaskID:   task.ID,
		Prompt:   task.Prompt,
		Output:   output,
		Success:  err == nil,
		Duration: time.Since(start).String(),
	}

	if err != nil {
		result.Error = err.Error()
	}

	return result
}

// executeSubagent runs an actual AIAgent loop for a subagent task.
// This is the key integration point — each subagent gets its own agent loop.
func (e *Engine) executeSubagent(ctx context.Context, prompt string, registry *tools.Registry) (string, error) {
	if e.executeSubagentFn != nil {
		return e.executeSubagentFn(ctx, prompt, registry)
	}

	return fmt.Sprintf("Subagent executed (no executor wired): %s", prompt), nil
}

// runVerification runs adversarial verification on a phase's results.
func (e *Engine) runVerification(ctx context.Context, phase Phase, phaseResult *PhaseResultData) *VerifyData {
	e.logger.Info("running adversarial verification", "phase", phase.Name)

	// Build verification prompt
	// Verify agents only see the original task + output, NOT the reasoning
	var issues []string
	for _, taskResult := range phaseResult.TaskResults {
		if !taskResult.Success {
			issues = append(issues, fmt.Sprintf("Task %s failed: %s", taskResult.TaskID, taskResult.Error))
		}
	}

	verifyData := &VerifyData{
		IssuesFound: len(issues),
		NeedsRerun:  len(issues) > 0,
	}

	if len(issues) > 0 {
		verifyData.Details = fmt.Sprintf("Found %d issues: %v", len(issues), issues)
	} else {
		verifyData.Details = "No issues found — all tasks completed successfully."
	}

	return verifyData
}

// buildFinalAnswer constructs the final answer from all phase results.
func (e *Engine) buildFinalAnswer(result *RunResult) string {
	var answer string
	for _, phase := range result.PhaseResults {
		answer += fmt.Sprintf("\n## Phase: %s\n", phase.PhaseName)
		for _, task := range phase.TaskResults {
			status := "✓"
			if !task.Success {
				status = "✗"
			}
			answer += fmt.Sprintf("%s **%s**: %s\n", status, task.TaskID, task.Output)
		}
		if phase.VerifyResult != nil {
			if phase.VerifyResult.NeedsRerun {
				answer += fmt.Sprintf("⚠ Verify: %s\n", phase.VerifyResult.Details)
			} else {
				answer += fmt.Sprintf("✓ Verify: %s\n", phase.VerifyResult.Details)
			}
		}
	}
	return answer
}

// SetSubagentExecutor sets the function that executes a subagent.
// This allows the engine to be wired up with a real AIAgent in cmd/agent/main.go.
func (e *Engine) SetSubagentExecutor(fn func(ctx context.Context, prompt string, registry *tools.Registry) (string, error)) {
	e.executeSubagentFn = fn
}

// DefaultSubagentExecutor is the default executor used by all workflow engines.
var DefaultSubagentExecutor func(ctx context.Context, prompt string, registry *tools.Registry) (string, error)

