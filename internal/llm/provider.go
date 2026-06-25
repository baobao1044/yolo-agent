package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// Provider is the interface for LLM providers.
type Provider interface {
	// Chat sends messages and returns the assistant's response.
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)

	// ChatStream sends messages and returns a stream of responses.
	ChatStream(ctx context.Context, req *ChatRequest) (<-chan ChatStreamChunk, error)

	// Name returns the provider name.
	Name() string
}

// ChatRequest is a request to the LLM.
type ChatRequest struct {
	Messages   []ChatMessage     `json:"messages"`
	Tools      []ToolDefinition  `json:"tools,omitempty"`
	MaxTokens  int               `json:"max_tokens"`
	Temperature float64          `json:"temperature"`
	Model      string            `json:"model"`
}

// ChatMessage is a message in the chat.
type ChatMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []ChatToolCall   `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	Name       string           `json:"name,omitempty"`
}

// ChatToolCall is a tool call from the LLM.
type ChatToolCall struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolDefinition defines a tool for the LLM.
type ToolDefinition struct {
	Type     string       `json:"type"`
	Function FunctionDef  `json:"function"`
}

// FunctionDef defines a function tool.
type FunctionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ChatResponse is the LLM's response.
type ChatResponse struct {
	Choices    []ChatMessage `json:"choices"`
	Usage      Usage         `json:"usage"`
	StopReason string        `json:"stop_reason"`
}

// Usage tracks token usage.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// ChatStreamChunk is a chunk in the streaming response.
type ChatStreamChunk struct {
	Delta      *ChatMessage `json:"delta,omitempty"`
	Finished  bool         `json:"finished"`
	Error     error        `json:"-"`
}

// OpenAICompatibleProvider implements Provider using OpenAI-compatible APIs.
type OpenAICompatibleProvider struct {
	baseURL    string
	apiKey     string
	model      string
	maxTokens  int
	temperature float64
	httpClient HTTPClient
}

// HTTPClient is an interface for making HTTP requests.
type HTTPClient interface {
	Do(req *HTTPRequest) (*HTTPResponse, error)
}

// HTTPRequest is a simplified HTTP request.
type HTTPRequest struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    io.Reader
}

// HTTPResponse is a simplified HTTP response.
type HTTPResponse struct {
	StatusCode int
	Body       io.ReadCloser
}

// NewOpenAICompatibleProvider creates a new OpenAI-compatible provider.
func NewOpenAICompatibleProvider(baseURL, apiKey, model string, maxTokens int, temperature float64) *OpenAICompatibleProvider {
	return &OpenAICompatibleProvider{
		baseURL:     baseURL,
		apiKey:      apiKey,
		model:       model,
		maxTokens:   maxTokens,
		temperature: temperature,
	}
}

func (p *OpenAICompatibleProvider) Name() string {
	return "openai-compatible"
}

// Chat sends a chat request using the OpenAI-compatible API.
func (p *OpenAICompatibleProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	// Build the request body
	body := map[string]any{
		"model":       p.model,
		"messages":    req.Messages,
		"max_tokens":  p.maxTokens,
		"temperature": p.temperature,
	}

	if len(req.Tools) > 0 {
		body["tools"] = req.Tools
		body["tool_choice"] = "auto"
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Use the actual OpenAI client
	return p.doChatRequest(ctx, jsonBody)
}

// ChatStream sends a streaming chat request.
func (p *OpenAICompatibleProvider) ChatStream(ctx context.Context, req *ChatRequest) (<-chan ChatStreamChunk, error) {
	// Streaming will be implemented with the actual go-openai client
	ch := make(chan ChatStreamChunk, 100)
	go func() {
		defer close(ch)
		resp, err := p.Chat(ctx, req)
		if err != nil {
			ch <- ChatStreamChunk{Error: err}
			return
		}
		if len(resp.Choices) > 0 {
			ch <- ChatStreamChunk{Delta: &resp.Choices[0], Finished: false}
		}
		ch <- ChatStreamChunk{Finished: true}
	}()
	return ch, nil
}

// doChatRequest performs the actual HTTP request to the LLM API.
// This will be replaced with the sashabaranov/go-openai client in the integration layer.
func (p *OpenAICompatibleProvider) doChatRequest(ctx context.Context, jsonBody []byte) (*ChatResponse, error) {
	// Placeholder — actual implementation uses go-openai
	return nil, fmt.Errorf("doChatRequest: use the integration layer with sashabaranov/go-openai")
}
