package workflow

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/baobg/yolo-agent/internal/tools"
)

// VerifyLoop implements the adversarial verification pattern.
// It runs independent verify agents that check the work of change agents,
// and can trigger re-runs if issues are found.
type VerifyLoop struct {
	registry     *tools.Registry
	maxRetries   int
	logger       *slog.Logger
	executeAgent func(ctx context.Context, prompt string, registry *tools.Registry) (string, error)
}

// NewVerifyLoop creates a new adversarial verify loop.
func NewVerifyLoop(registry *tools.Registry, maxRetries int, logger *slog.Logger) *VerifyLoop {
	if logger == nil {
		logger = slog.Default()
	}
	if maxRetries == 0 {
		maxRetries = 2
	}
	return &VerifyLoop{
		registry:   registry,
		maxRetries: maxRetries,
		logger:      logger,
	}
}

// SetExecuteAgent sets the agent executor function.
func (v *VerifyLoop) SetExecuteAgent(fn func(ctx context.Context, prompt string, registry *tools.Registry) (string, error)) {
	v.executeAgent = fn
}

// VerifyResult holds the result of a verification run.
type VerifyResult struct {
	Passed      bool   `json:"passed"`
	IssuesFound int    `json:"issues_found"`
	Details     string `json:"details"`
	RetriesUsed int    `json:"retries_used"`
}

// Run executes the Understand → Change → Verify loop with retries.
//
// originalState: description of the state before changes
// changePhase: the phase that makes changes
// verifyPhase: the phase that verifies changes
//
// The verify agents only see the original state + change outputs (diffs),
// NOT the internal reasoning of the change agents.
func (v *VerifyLoop) Run(
	ctx context.Context,
	originalState string,
	changePhase Phase,
	verifyPhase Phase,
) (*VerifyResult, error) {
	result := &VerifyResult{}
	var changeOutput []TaskResultData

	for attempt := 0; attempt <= v.maxRetries; attempt++ {
		result.RetriesUsed = attempt

		// Run change phase
		v.logger.Info("verify loop: running change phase", "attempt", attempt)

		// Execute change tasks
		for _, task := range changePhase.Tasks {
			output, err := v.executeAgent(ctx, task.Prompt, v.registry.Filter(task.Tools))
			taskResult := TaskResultData{
				TaskID:  task.ID,
				Prompt:  task.Prompt,
				Output:  output,
				Success: err == nil,
			}
			if err != nil {
				taskResult.Error = err.Error()
			}
			changeOutput = append(changeOutput, taskResult)
		}

		// Build diff summary for verify agents
		diffSummary := buildDiffSummary(originalState, changeOutput)

		// Run verify phase
		// Verify agents are independent — they only see originalState + diff
		v.logger.Info("verify loop: running verification", "attempt", attempt)

		var issues []string
		for _, task := range verifyPhase.Tasks {
			// Inject the diff summary into the verify prompt
			verifyPrompt := fmt.Sprintf(
				"## Original State\n%s\n\n## Changes Made\n%s\n\n## Your Task\n%s\n\nFind problems, edge cases, regressions, or anything that could go wrong.",
				originalState, diffSummary, task.Prompt,
			)

			verifyOutput, err := v.executeAgent(ctx, verifyPrompt, v.registry.Filter(task.Tools))
			if err != nil {
				issues = append(issues, fmt.Sprintf("Verify agent error: %v", err))
				continue
			}

			// If the verify agent found issues, they'll be in the output
			if containsIssueKeywords(verifyOutput) {
				issues = append(issues, verifyOutput)
			}
		}

		result.IssuesFound = len(issues)

		if len(issues) == 0 {
			// All clear!
			result.Passed = true
			result.Details = "Adversarial verification passed — no issues found."
			return result, nil
		}

		// Issues found — will retry if we have retries left
		v.logger.Info("verify loop: issues found", "count", len(issues), "attempt", attempt)
		result.Details = fmt.Sprintf("Found %d issues on attempt %d: %v", len(issues), attempt, issues)

		if attempt < v.maxRetries {
			// Re-run change phase with feedback
			// Add verify feedback to the change prompts
			for i := range changePhase.Tasks {
				changePhase.Tasks[i].Prompt = fmt.Sprintf(
					"%s\n\n## Previous attempt issues:\n%s\n\nPlease fix these issues.",
					changePhase.Tasks[i].Prompt,
					formatIssues(issues),
				)
			}
			changeOutput = nil // Reset for re-run
		}
	}

	// Exhausted retries
	result.Passed = false
	result.Details = fmt.Sprintf("Verification failed after %d retries: %s", result.RetriesUsed, result.Details)
	return result, nil
}

// buildDiffSummary creates a summary of changes for verify agents.
// Verify agents only see diffs, not internal reasoning.
func buildDiffSummary(originalState string, changeResults []TaskResultData) string {
	summary := fmt.Sprintf("Original state: %s\n\nChanges:\n", originalState)
	for _, r := range changeResults {
		status := "✓"
		if !r.Success {
			status = "✗"
		}
		summary += fmt.Sprintf("- %s %s: %s\n", status, r.TaskID, r.Output)
	}
	return summary
}

// containsIssueKeywords checks if the verify output contains issue indicators.
func containsIssueKeywords(output string) bool {
	keywords := []string{"issue", "problem", "error", "bug", "regression", "broken", "fail", "wrong", "missing", "incorrect"}
	for _, kw := range keywords {
		if contains(output, kw) {
			return true
		}
	}
	return false
}

// contains checks if s contains substr (case-insensitive).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && search(s, substr)
}

func search(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// formatIssues formats a list of issues for feedback.
func formatIssues(issues []string) string {
	result := ""
	for i, issue := range issues {
		result += fmt.Sprintf("%d. %s\n", i+1, issue)
	}
	return result
}
