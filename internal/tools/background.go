// Copyright (c) 2026 Bao Bui Gia / YOLO Agent contributors
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/baobg/yolo-agent/internal/background"
)

// ScheduleTool lets the agent create background tasks that run inside permission envelopes.
type ScheduleTool struct {
	scheduler *background.Scheduler
}

// NewScheduleTool creates a tool bound to the background scheduler.
func NewScheduleTool(scheduler *background.Scheduler) *ScheduleTool {
	return &ScheduleTool{scheduler: scheduler}
}

// Name returns the tool name.
func (t *ScheduleTool) Name() string {
	return "schedule_task"
}

// Description returns the tool description.
func (t *ScheduleTool) Description() string {
	return "Schedule an autonomous background task with a permission envelope. Provide an instruction, schedule (e.g. '@once', '@interval 5m', cron expression), allowed tools, and budgets. The task will run without blocking the chat."
}

// Schema returns the JSON Schema for the tool.
func (t *ScheduleTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"name": {
				Type:        "string",
				Description: "Short human-readable task name.",
			},
			"instruction": {
				Type:        "string",
				Description: "Detailed task instruction. This is what the agent runs in the background.",
			},
			"schedule": {
				Type:        "string",
				Description: "Schedule expression: '@once' for immediate one-shot, '@interval 5m', '@hourly', '@daily', or a cron expression with seconds field.",
				Default:     "@once",
			},
			"allowed_tools": {
				Type:        "array",
				Description: "List of tool names the background task is allowed to use. Use ['*'] to allow all, but prefer an explicit list for safety.",
				// Items description is not supported by this simplified schema; keep it simple.
			},
			"denied_tools": {
				Type:        "array",
				Description: "Tool names explicitly forbidden.",
			},
			"max_tokens": {
				Type:        "integer",
				Description: "Maximum LLM tokens this task may consume. 0 means unlimited.",
				Default:     100000,
			},
			"max_calls": {
				Type:        "integer",
				Description: "Maximum tool calls this task may execute. 0 means unlimited.",
				Default:     100,
			},
			"max_duration": {
				Type:        "integer",
				Description: "Maximum execution time in seconds. 0 means unlimited.",
				Default:     300,
			},
			"notify_channels": {
				Type:        "array",
				Description: "Channels to notify on finish/block/error: tui, telegram, discord, slack, email.",
			},
		},
		Required: []string{"name", "instruction", "schedule"},
	}
}

// Execute creates and schedules a background envelope.
func (t *ScheduleTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct {
		Name           string   `json:"name"`
		Instruction    string   `json:"instruction"`
		Schedule       string   `json:"schedule"`
		AllowedTools   []string `json:"allowed_tools"`
		DeniedTools    []string `json:"denied_tools"`
		MaxTokens      int      `json:"max_tokens"`
		MaxCalls       int      `json:"max_calls"`
		MaxDuration    int      `json:"max_duration"`
		NotifyChannels []string `json:"notify_channels"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, fmt.Errorf("parse args: %w", err)
	}

	if req.Name == "" || req.Instruction == "" || req.Schedule == "" {
		return nil, fmt.Errorf("name, instruction, and schedule are required")
	}

	env := background.DefaultEnvelope()
	env.Name = req.Name
	env.Instruction = req.Instruction
	env.Schedule = req.Schedule
	env.Scope.AllowedTools = req.AllowedTools
	env.Scope.DeniedTools = req.DeniedTools
	if len(env.Scope.AllowedTools) == 0 {
		env.Scope.AllowedTools = []string{"respond", "terminal", "browser"}
	}

	if req.MaxTokens > 0 {
		env.Budget.MaxTokens = req.MaxTokens
	}
	if req.MaxCalls > 0 {
		env.Budget.MaxCalls = req.MaxCalls
	}
	if req.MaxDuration > 0 {
		env.Budget.MaxDuration = req.MaxDuration
	}
	if len(req.NotifyChannels) > 0 {
		env.Notify.Channels = req.NotifyChannels
	}

	if err := env.Validate(); err != nil {
		return nil, err
	}

	if err := t.scheduler.Schedule(ctx, env); err != nil {
		return nil, fmt.Errorf("schedule failed: %w", err)
	}

	return map[string]interface{}{
		"id":          env.ID,
		"name":        env.Name,
		"schedule":    env.Schedule,
		"status":      env.Status,
		"next_run_at": env.NextRunAt,
		"message":     "Background task scheduled. Use task controls to pause, resume, or kill it.",
	}, nil
}

// EnvelopeStateTool returns the state of a background envelope.
type EnvelopeStateTool struct {
	store *background.Store
}

// NewEnvelopeStateTool creates a state inspector tool.
func NewEnvelopeStateTool(store *background.Store) *EnvelopeStateTool {
	return &EnvelopeStateTool{store: store}
}

// Name returns the tool name.
func (t *EnvelopeStateTool) Name() string {
	return "task_status"
}

// Description returns the tool description.
func (t *EnvelopeStateTool) Description() string {
	return "Get the status of background tasks and their consumed budgets."
}

// Schema returns the JSON schema.
func (t *EnvelopeStateTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"id": {
				Type:        "string",
				Description: "Envelope ID. Omit to list all tasks.",
			},
		},
	}
	// Required intentionally empty.
}

// Execute returns envelope state.
func (t *EnvelopeStateTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(args, &req)

	if req.ID != "" {
		env, err := t.store.Get(req.ID)
		if err != nil {
			return nil, err
		}
		return env.Snapshot(), nil
	}

	envs, err := t.store.List()
	if err != nil {
		return nil, err
	}

	var out []background.Envelope
	for _, env := range envs {
		out = append(out, env.Snapshot())
	}
	return out, nil
}
