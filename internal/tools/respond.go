package tools

import (
	"context"
	"encoding/json"
)

// RespondTool is the tool the LLM calls to give the final response to the user.
// This terminates the agent loop.
type RespondTool struct{}

// NewRespondTool creates a new RespondTool.
func NewRespondTool() *RespondTool {
	return &RespondTool{}
}

func (t *RespondTool) Name() string {
	return "respond"
}

func (t *RespondTool) Description() string {
	return "Send your final response to the user. Call this when you have completed the task and are ready to reply. This ends the current agent loop."
}

func (t *RespondTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"message": {
				Type:        "string",
				Description: "Your final response to the user.",
			},
		},
		Required: []string{"message"},
	}
}

// RespondArgs is the parsed arguments for the respond tool.
type RespondArgs struct {
	Message string `json:"message"`
}

// RespondResult is the result of the respond tool.
type RespondResult struct {
	Message string `json:"message"`
	Done    bool   `json:"done"`
}

func (t *RespondTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var parsed RespondArgs
	if err := json.Unmarshal(args, &parsed); err != nil {
		return nil, err
	}
	return RespondResult{
		Message: parsed.Message,
		Done:    true,
	}, nil
}
