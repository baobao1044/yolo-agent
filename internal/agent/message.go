package agent

import "encoding/json"

// Role represents the role of a message sender.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message represents a single message in the conversation.
type Message struct {
	Role       Role            `json:"role"`
	Content    string          `json:"content,omitempty"`
	ToolCalls  []ToolCall      `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"` // for RoleTool responses
	Name       string          `json:"name,omitempty"`          // tool name for RoleTool
}

// ToolCall represents a single tool call from the LLM.
type ToolCall struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolResult is the result of executing a tool.
type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error"`
}

// Conversation represents a full conversation history.
type Conversation struct {
	ID       string    `json:"id"`
	Messages []Message `json:"messages"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ToOpenAIMessages converts messages to OpenAI-compatible format.
func (c *Conversation) ToOpenAIMessages() []map[string]any {
	var msgs []map[string]any
	for _, m := range c.Messages {
		msg := map[string]any{
			"role": string(m.Role),
		}

		if m.Role == RoleTool {
			msg["content"] = m.Content
			msg["tool_call_id"] = m.ToolCallID
		} else if len(m.ToolCalls) > 0 {
			msg["content"] = m.Content
			var calls []map[string]any
			for _, tc := range m.ToolCalls {
				calls = append(calls, map[string]any{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]any{
						"name":      tc.Name,
						"arguments": string(tc.Arguments),
					},
				})
			}
			msg["tool_calls"] = calls
		} else {
			msg["content"] = m.Content
		}

		msgs = append(msgs, msg)
	}
	return msgs
}
