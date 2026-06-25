package workflow

import (
	"encoding/json"
	"fmt"
)

// OrchestrationPlan defines the structure of a multi-agent workflow.
type OrchestrationPlan struct {
	Phases []Phase `json:"phases"`
}

// Phase is a stage in the orchestration plan.
type Phase struct {
	Name      string   `json:"name"`
	Parallel  bool     `json:"parallel"`
	Adversarial bool   `json:"adversarial"`
	Tasks     []Task   `json:"tasks"`
	DependsOn []string `json:"depends_on"` // names of phases this depends on
}

// Task is a single unit of work within a phase.
type Task struct {
	ID     string   `json:"id"`
	Prompt string   `json:"prompt"`
	Tools  []string `json:"tools"` // allowed tools for this subagent
}

// ParsePlan parses a JSON orchestration plan.
func ParsePlan(raw json.RawMessage) (*OrchestrationPlan, error) {
	var plan OrchestrationPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return nil, fmt.Errorf("unmarshal plan: %w", err)
	}
	return &plan, nil
}

// Validate checks the orchestration plan for errors.
func (p *OrchestrationPlan) Validate() error {
	if len(p.Phases) == 0 {
		return fmt.Errorf("plan must have at least one phase")
	}

	phaseNames := make(map[string]bool)
	for i, phase := range p.Phases {
		if phase.Name == "" {
			return fmt.Errorf("phase %d must have a name", i)
		}
		if phaseNames[phase.Name] {
			return fmt.Errorf("duplicate phase name: %s", phase.Name)
		}
		phaseNames[phase.Name] = true

		if len(phase.Tasks) == 0 {
			return fmt.Errorf("phase %q must have at least one task", phase.Name)
		}

		for j, task := range phase.Tasks {
			if task.Prompt == "" {
				return fmt.Errorf("task %d in phase %q must have a prompt", j, phase.Name)
			}
		}

		// Check dependency references
		for _, dep := range phase.DependsOn {
			if !phaseNames[dep] {
				// Check if it's defined earlier
				found := false
				for k := 0; k < i; k++ {
					if p.Phases[k].Name == dep {
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("phase %q depends on unknown phase %q", phase.Name, dep)
				}
			}
		}
	}

	return nil
}

// TotalTasks returns the total number of tasks across all phases.
func (p *OrchestrationPlan) TotalTasks() int {
	total := 0
	for _, phase := range p.Phases {
		total += len(phase.Tasks)
	}
	return total
}
