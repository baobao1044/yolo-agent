package tools

import (
	"context"
	"encoding/json"
)

// ToolSchema describes a tool's input schema in JSON Schema format.
type ToolSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]SchemaProperty `json:"properties,omitempty"`
	Required   []string                  `json:"required,omitempty"`
}

// SchemaProperty describes a single property in a tool schema.
type SchemaProperty struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Default     any      `json:"default,omitempty"`
}

// Tool is the interface every tool must implement.
type Tool interface {
	// Name returns the unique tool identifier.
	Name() string

	// Description returns a human-readable description of the tool.
	Description() string

	// Schema returns the JSON Schema for the tool's input parameters.
	Schema() ToolSchema

	// Execute runs the tool with the given arguments.
	Execute(ctx context.Context, args json.RawMessage) (any, error)
}

// ToolDefinition is the OpenAI-compatible tool definition format.
type ToolDefinition struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

// Function describes a function tool for the OpenAI API.
type Function struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Parameters  ToolSchema `json:"parameters"`
}

// ToDefinition converts a Tool to an OpenAI-compatible ToolDefinition.
func ToDefinition(t Tool) ToolDefinition {
	return ToolDefinition{
		Type: "function",
		Function: Function{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Schema(),
		},
	}
}
