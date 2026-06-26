package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	openai "github.com/sashabaranov/go-openai"
)

// OpenAIClient wraps the sashabaranov/go-openai client.
type OpenAIClient struct {
	client      *openai.Client
	model       string
	maxTokens   int
	temperature float32
	logger      *slog.Logger
}

// NewOpenAIClient creates a new OpenAI-compatible client.
func NewOpenAIClient(baseURL, apiKey, model string, maxTokens int, temperature float32) *OpenAIClient {
	config := openai.DefaultConfig(apiKey)
	if baseURL != "" {
		config.BaseURL = baseURL
	}

	return &OpenAIClient{
		client:      openai.NewClientWithConfig(config),
		model:       model,
		maxTokens:   maxTokens,
		temperature: temperature,
		logger:      slog.Default(),
	}
}

// Chat sends a chat completion request and returns an llm.ChatResponse.
func (c *OpenAIClient) Chat(ctx context.Context, messages []ChatMessage, tools []ToolDefinition) (*ChatResponse, error) {
	req := openai.ChatCompletionRequest{
		Model:       c.model,
		Messages:    convertToOpenAIMessages(messages),
		MaxTokens:   c.maxTokens,
		Temperature: c.temperature,
		Tools:       convertToOpenAITools(tools),
		ToolChoice:  "auto",
	}

	resp, err := c.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("chat completion: %w", err)
	}

	return convertFromOpenAIResponse(resp), nil
}

// convertToOpenAIMessages converts internal messages to OpenAI format.
func convertToOpenAIMessages(msgs []ChatMessage) []openai.ChatCompletionMessage {
	var result []openai.ChatCompletionMessage
	for _, m := range msgs {
		msg := openai.ChatCompletionMessage{
			Role: m.Role,
		}

		if m.Content != "" {
			msg.Content = m.Content
		}

		if m.ToolCallID != "" {
			msg.ToolCallID = m.ToolCallID
		}

		if m.Name != "" {
			msg.Name = m.Name
		}

		if len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				msg.ToolCalls = append(msg.ToolCalls, openai.ToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: openai.FunctionCall{
						Name:      tc.Name,
						Arguments: tc.Arguments,
					},
				})
			}
		}

		result = append(result, msg)
	}
	return result
}

// convertToOpenAITools converts internal tool definitions to OpenAI format.
func convertToOpenAITools(tools []ToolDefinition) []openai.Tool {
	var result []openai.Tool
	for _, t := range tools {
		result = append(result, openai.Tool{
			Type: openai.ToolType(t.Type),
			Function: &openai.FunctionDefinition{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:   t.Function.Parameters,
			},
		})
	}
	return result
}

// convertFromOpenAIResponse converts an OpenAI response to internal ChatResponse.
func convertFromOpenAIResponse(resp openai.ChatCompletionResponse) *ChatResponse {
	result := &ChatResponse{
		Usage: Usage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		},
		StopReason: string(resp.Choices[0].FinishReason),
		Choices:    make([]ChatMessage, 0, len(resp.Choices)),
	}

	for _, choice := range resp.Choices {
		msg := ChatMessage{
			Role:    choice.Message.Role,
			Content: choice.Message.Content,
		}

		for _, tc := range choice.Message.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, ChatToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			})
		}

		result.Choices = append(result.Choices, msg)
	}

	return result
}

// Marshal is the JSON formatter used by memory store (avoid fmt.Marshal conflict).
func Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

// CreateEmbeddings calls the OpenAI-compatible /v1/embeddings endpoint for a
// batch of texts. It satisfies the embeddings.LLMEmbedder interface. When
// model is empty, the client's configured chat model is used (callers should
// pass a dedicated embedding model name instead).
func (c *OpenAIClient) CreateEmbeddings(ctx context.Context, texts []string, model string) ([][]float32, error) {
	req := openai.EmbeddingRequest{
		Input: texts,
	}
	if model != "" {
		req.Model = openai.EmbeddingModel(model)
	} else {
		req.Model = openai.EmbeddingModel(c.model)
	}

	resp, err := c.client.CreateEmbeddings(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("create embeddings: %w", err)
	}

	out := make([][]float32, len(resp.Data))
	for i, d := range resp.Data {
		// The API may return embeddings out of index order; sort by Index if present.
		if int(d.Index) < len(out) {
			out[d.Index] = d.Embedding
		} else {
			out[i] = d.Embedding
		}
	}
	return out, nil
}
