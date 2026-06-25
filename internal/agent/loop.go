package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/baobg/yolo-agent/internal/llm"
	"github.com/baobg/yolo-agent/internal/tools"
)

// LLMClient is the interface the agent uses to call the LLM.
type LLMClient interface {
	Chat(ctx context.Context, messages []llm.ChatMessage, tools []llm.ToolDefinition) (*llm.ChatResponse, error)
}

// AIAgent is the core agent that runs the conversation loop.
type AIAgent struct {
	client       LLMClient
	registry     *tools.Registry
	conv         *Conversation
	systemPrompt string
	maxIter      int
	logger       *slog.Logger
}

// NewAIAgent creates a new AI agent.
func NewAIAgent(client LLMClient, registry *tools.Registry, systemPrompt string, maxIter int, logger *slog.Logger) *AIAgent {
	if logger == nil {
		logger = slog.Default()
	}
	return &AIAgent{
		client:       client,
		registry:     registry,
		conv:         &Conversation{Messages: []Message{}},
		systemPrompt: systemPrompt,
		maxIter:      maxIter,
		logger:       logger,
	}
}

// SetConversation sets the conversation history.
func (a *AIAgent) SetConversation(conv *Conversation) {
	a.conv = conv
}

// Conversation returns the current conversation.
func (a *AIAgent) Conversation() *Conversation {
	return a.conv
}

// AddMessage adds a message to the conversation.
func (a *AIAgent) AddMessage(msg Message) {
	a.conv.Messages = append(a.conv.Messages, msg)
}

// Run executes the agent loop: send messages to LLM, execute tools, loop until respond or max iterations.
// Returns the final response and any error.
func (a *AIAgent) Run(ctx context.Context, userInput string) (string, error) {
	// Add user message
	a.conv.Messages = append(a.conv.Messages, Message{
		Role:    RoleUser,
		Content: userInput,
	})

	return a.loop(ctx)
}

// RunWithHistory runs the agent loop without adding a new user message (continues from existing history).
func (a *AIAgent) RunWithHistory(ctx context.Context) (string, error) {
	return a.loop(ctx)
}

// loop is the main agent loop.
func (a *AIAgent) loop(ctx context.Context) (string, error) {
	for i := 0; i < a.maxIter; i++ {
		a.logger.Debug("agent loop iteration", "iteration", i)

		// Build LLM messages
		llmMsgs := a.buildLLMMessages()

		// Build tool definitions
		toolDefs := a.buildToolDefinitions()

		// Call LLM
		resp, err := a.client.Chat(ctx, llmMsgs, toolDefs)
		if err != nil {
			return "", fmt.Errorf("LLM call failed: %w", err)
		}

		if len(resp.Choices) == 0 {
			return "", fmt.Errorf("LLM returned no choices")
		}

		choice := resp.Choices[0]

		// Add assistant message to conversation
		msg := Message{
			Role:    RoleAssistant,
			Content: choice.Content,
		}

		// Check if there are tool calls
		if len(choice.ToolCalls) > 0 {
			for _, tc := range choice.ToolCalls {
				msg.ToolCalls = append(msg.ToolCalls, ToolCall{
					ID:        tc.ID,
					Name:      tc.Name,
					Arguments: json.RawMessage(tc.Arguments),
				})
			}
			a.conv.Messages = append(a.conv.Messages, msg)

			// Execute each tool call
			for _, tc := range choice.ToolCalls {
				a.logger.Info("executing tool", "name", tc.Name, "id", tc.ID)

				result, err := a.registry.Execute(ctx, tc.Name, json.RawMessage(tc.Arguments))

				var content string
				var isError bool
				if err != nil {
					content = fmt.Sprintf("Error: %v", err)
					isError = true
				} else {
					resultJSON, _ := json.Marshal(result)
					content = string(resultJSON)

					// Check if this is the respond tool
					if tc.Name == "respond" {
						var respResult tools.RespondResult
						if jsonErr := json.Unmarshal(resultJSON, &respResult); jsonErr == nil && respResult.Done {
							a.conv.Messages = append(a.conv.Messages, Message{
								Role:       RoleTool,
								ToolCallID: tc.ID,
								Name:       tc.Name,
								Content:    content,
							})
							return respResult.Message, nil
						}
					}

					// Check if this is the orchestrate tool
					if tc.Name == "orchestrate" {
						a.conv.Messages = append(a.conv.Messages, Message{
							Role:       RoleTool,
							ToolCallID: tc.ID,
							Name:       tc.Name,
							Content:    content,
						})
						// The orchestrator will handle this at a higher level
						return content, nil
					}
				}

				// Add tool result to conversation
				if isError {
					content = fmt.Sprintf(`{"error":%q}`, err.Error())
				}
				a.conv.Messages = append(a.conv.Messages, Message{
					Role:       RoleTool,
					ToolCallID: tc.ID,
					Name:       tc.Name,
					Content:    content,
				})
			}
			// Continue loop — LLM will see tool results and decide next action
			continue
		}

		// No tool calls — return the assistant's text response
		if choice.Content != "" {
			return choice.Content, nil
		}

		// Empty response with no tool calls
		return "", fmt.Errorf("LLM returned empty response with no tool calls")
	}

	return "", fmt.Errorf("agent loop exceeded max iterations (%d)", a.maxIter)
}

// buildLLMMessages converts the conversation to LLM messages.
func (a *AIAgent) buildLLMMessages() []llm.ChatMessage {
	msgs := []llm.ChatMessage{
		{
			Role:    "system",
			Content: a.systemPrompt,
		},
	}

	for _, m := range a.conv.Messages {
		msg := llm.ChatMessage{
			Role: string(m.Role),
		}

		switch m.Role {
		case RoleSystem, RoleUser, RoleAssistant:
			msg.Content = m.Content
			if len(m.ToolCalls) > 0 {
				for _, tc := range m.ToolCalls {
					msg.ToolCalls = append(msg.ToolCalls, llm.ChatToolCall{
						ID:        tc.ID,
						Name:      tc.Name,
						Arguments: string(tc.Arguments),
					})
				}
			}
		case RoleTool:
			msg.Content = m.Content
			msg.ToolCallID = m.ToolCallID
			msg.Name = m.Name
		}

		msgs = append(msgs, msg)
	}

	return msgs
}

// buildToolDefinitions converts registered tools to LLM tool definitions.
func (a *AIAgent) buildToolDefinitions() []llm.ToolDefinition {
	toolDefs := a.registry.Definitions()
	var result []llm.ToolDefinition
	for _, td := range toolDefs {
		params := map[string]any{
			"type":       td.Function.Parameters.Type,
			"properties": td.Function.Parameters.Properties,
			"required":   td.Function.Parameters.Required,
		}
		result = append(result, llm.ToolDefinition{
			Type: "function",
			Function: llm.FunctionDef{
				Name:        td.Function.Name,
				Description: td.Function.Description,
				Parameters:  params,
			},
		})
	}
	return result
}
