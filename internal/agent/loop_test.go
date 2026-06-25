package agent

import (
	"context"
	"testing"

	"github.com/baobg/yolo-agent/internal/llm"
	"github.com/baobg/yolo-agent/internal/tools"
)

// fakeLLM is a mock LLM client that returns scripted responses.
type fakeLLM struct {
	responses []llm.ChatResponse
	callIndex int
}

func (f *fakeLLM) Chat(ctx context.Context, messages []llm.ChatMessage, tools []llm.ToolDefinition) (*llm.ChatResponse, error) {
	if f.callIndex >= len(f.responses) {
		return &llm.ChatResponse{
			Choices: []llm.ChatMessage{
				{
					Role:    "assistant",
					Content: "default response",
				},
			},
		}, nil
	}
	resp := &f.responses[f.callIndex]
	f.callIndex++
	return resp, nil
}

func TestAIAgent_RunDirect(t *testing.T) {
	// Fake LLM returns a simple text response without tool calls.
	llmClient := &fakeLLM{
		responses: []llm.ChatResponse{
			{
				Choices: []llm.ChatMessage{
					{
						Role:    "assistant",
						Content: "Hello, human!",
					},
				},
			},
		},
	}

	reg := tools.NewRegistry()
	reg.MustRegister(tools.NewRespondTool())

	agent := NewAIAgent(llmClient, reg, "You are helpful.", 10, nil)

	resp, err := agent.Run(context.Background(), "say hello")
	if err != nil {
		t.Fatalf("agent.Run failed: %v", err)
	}

	if resp != "Hello, human!" {
		t.Fatalf("expected 'Hello, human!', got %q", resp)
	}
}

func TestAIAgent_RespondTool(t *testing.T) {
	// Fake LLM calls respond tool.
	llmClient := &fakeLLM{
		responses: []llm.ChatResponse{
			{
				Choices: []llm.ChatMessage{
					{
						Role: "assistant",
						ToolCalls: []llm.ChatToolCall{
							{
								ID:        "call-1",
								Name:      "respond",
								Arguments: `{"message":"Done!"}`,
							},
						},
					},
				},
			},
		},
	}

	reg := tools.NewRegistry()
	reg.MustRegister(tools.NewRespondTool())

	agent := NewAIAgent(llmClient, reg, "You are helpful.", 10, nil)

	resp, err := agent.Run(context.Background(), " terminate with Done!")
	if err != nil {
		t.Fatalf("agent.Run failed: %v", err)
	}

	if resp != "Done!" {
		t.Fatalf("expected 'Done!', got %q", resp)
	}
}
