package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/baobg/yolo-agent/internal/workflow"
)

// WorkflowTUI displays workflow execution progress.
type WorkflowTUI struct {
	width      int
	height     int
	ready      bool
	result     *workflow.RunResult
	running   bool
}

// NewWorkflowTUI creates a new workflow TUI.
func NewWorkflowTUI() *WorkflowTUI {
	return &WorkflowTUI{}
}

// Init initializes the workflow TUI.
func (w *WorkflowTUI) Init() tea.Cmd {
	return tea.EnterAltScreen
}

// Update handles messages.
func (w *WorkflowTUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		w.width = msg.Width
		w.height = msg.Height
		w.ready = true

	case tea.KeyMsg:
		if msg.Type == tea.KeyEsc || msg.String() == "q" {
			return w, tea.Quit
		}

	case *workflow.RunResult:
		w.result = msg
		w.running = false
	}

	return w, nil
}

// View renders the workflow TUI.
func (w *WorkflowTUI) View() string {
	if !w.ready {
		return "Loading workflow view..."
	}

	var b strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00D8FF"))
	b.WriteString(titleStyle.Render(" YOLO Agent — Workflow View ") + "\n\n")

	if w.running {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFF00")).Render("Workflow running...") + "\n\n")
	}

	if w.result == nil {
		b.WriteString("No workflow data yet.\n")
		b.WriteString("Press ESC or Q to quit.\n")
		return b.String()
	}

	// Summary
	b.WriteString(fmt.Sprintf("Total agents: %d\n", w.result.TotalAgents))
	b.WriteString(fmt.Sprintf("Duration: %s\n\n", w.result.Duration))

	// Phases
	for _, phase := range w.result.PhaseResults {
		phaseStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF00FF"))
		b.WriteString(phaseStyle.Render(fmt.Sprintf("▶ Phase: %s", phase.PhaseName)) + "\n")

		for _, task := range phase.TaskResults {
			status := "✓"
			statusColor := "#00FF00"
			if !task.Success {
				status = "✗"
				statusColor = "#FF0000"
			}
			statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor))
				b.WriteString(fmt.Sprintf("  %s Task %s: %s (%.3s)\n",
				statusStyle.Render(status),
				task.TaskID,
				task.Prompt,
				task.Duration,
			))
			if task.Error != "" {
				b.WriteString(fmt.Sprintf("    Error: %s\n", task.Error))
			}
			if task.Output != "" {
				b.WriteString(fmt.Sprintf("    Output: %.300s\n", task.Output))
			}
		}

		if phase.VerifyResult != nil {
			if phase.VerifyResult.NeedsRerun {
				b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render(
					fmt.Sprintf("  ⚠ Verify: %s", phase.VerifyResult.Details),
				) + "\n")
			} else {
				b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00")).Render(
					fmt.Sprintf("  ✓ Verify: %s", phase.VerifyResult.Details),
				) + "\n")
			}
		}

		b.WriteString("\n")
	}

	// Final answer preview
	if w.result.FinalAnswer != "" {
		b.WriteString(lipgloss.NewStyle().Bold(true).Render("Final Answer:") + "\n")
		answer := w.result.FinalAnswer
		if len(answer) > 500 {
			answer = answer[:500] + "..."
		}
		b.WriteString(answer + "\n\n")
	}

	b.WriteString("Press ESC or Q to quit.\n")
	return b.String()
}

// SetResult sets the workflow result.
func (w *WorkflowTUI) SetResult(result *workflow.RunResult) {
	w.result = result
	w.running = false
}

// SetRunning sets the running state.
func (w *WorkflowTUI) SetRunning(running bool) {
	w.running = running
}
