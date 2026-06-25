package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/baobg/yolo-agent/internal/tools"
)

// ServerConfig holds configuration for one MCP server.
type ServerConfig struct {
	Name    string            `json:"name" yaml:"name"`
	Command string            `json:"command" yaml:"command"`
	Args    []string          `json:"args" yaml:"args"`
	Env     map[string]string `json:"env" yaml:"env"`
}

// Manager manages multiple MCP server clients and exposes their tools.
type Manager struct {
	servers  map[string]*Client
	config   []ServerConfig
	registry *tools.Registry
	logger   *slog.Logger
	mu       sync.RWMutex
}

// NewManager creates a new MCP manager.
func NewManager(config []ServerConfig, registry *tools.Registry, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		servers:  make(map[string]*Client),
		config:   config,
		registry: registry,
		logger:   logger,
	}
}

// ConnectAll starts all configured MCP servers and registers their tools.
func (m *Manager) ConnectAll(ctx context.Context) error {
	for _, sc := range m.config {
		if sc.Name == "" {
			sc.Name = sc.Command
		}
		if err := m.connectServer(ctx, sc); err != nil {
			m.logger.Error("failed to connect MCP server", "server", sc.Name, "error", err)
			// Continue with other servers; don't fail fast
		}
	}
	return nil
}

// connectServer connects a single MCP server and registers its tools.
func (m *Manager) connectServer(ctx context.Context, sc ServerConfig) error {
	client := NewClient(sc.Command, sc.Args, sc.Env, m.logger.With("mcp_server", sc.Name))
	if err := client.Connect(ctx); err != nil {
		return fmt.Errorf("connect %s: %w", sc.Name, err)
	}

	mcpTools, err := client.ListTools(ctx)
	if err != nil {
		_ = client.Close()
		return fmt.Errorf("list tools %s: %w", sc.Name, err)
	}

	m.mu.Lock()
	m.servers[sc.Name] = client
	m.mu.Unlock()

	for _, mt := range mcpTools {
		if err := m.registerMCPTool(sc.Name, client, mt); err != nil {
			m.logger.Error("failed to register MCP tool",
				"server", sc.Name,
				"tool", mt.Name,
				"error", err,
			)
		}
	}

	m.logger.Info("MCP server connected", "server", sc.Name, "tools", len(mcpTools))
	return nil
}

// registerMCPTool wraps an MCP tool as an internal tool.
func (m *Manager) registerMCPTool(serverName string, client *Client, mt MCPTool) error {
	tool := &mcptool{
		serverName: serverName,
		toolName:   mt.Name,
		desc:       mt.Description,
		schema:     mt.InputSchema,
		client:     client,
		logger:     m.logger,
	}

	if err := m.registry.Register(tool); err != nil {
		return err
	}

	m.logger.Debug("registered MCP tool",
		"server", serverName,
		"tool", tool.Name(),
	)
	return nil
}

// Close shuts down all MCP connections.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var firstErr error
	for name, client := range m.servers {
		if err := client.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("close %s: %w", name, err)
		}
	}
	return firstErr
}

// mcptool adapts an MCP server tool to the internal Tool interface.
type mcptool struct {
	serverName string
	toolName   string
	desc       string
	schema     map[string]any
	client     *Client
	logger     *slog.Logger
}

func (t *mcptool) Name() string {
	return fmt.Sprintf("mcp_%s_%s", t.serverName, t.toolName)
}

func (t *mcptool) Description() string {
	if t.desc == "" {
		return fmt.Sprintf("MCP tool %s from server %s", t.toolName, t.serverName)
	}
	return fmt.Sprintf("[%s] %s", t.serverName, t.desc)
}

func (t *mcptool) Schema() tools.ToolSchema {
	var ts tools.ToolSchema

	if t.schema != nil {
		if rawType, _ := t.schema["type"].(string); rawType != "" {
			ts.Type = rawType
		}
		if props, ok := t.schema["properties"].(map[string]any); ok {
			ts.Properties = make(map[string]tools.SchemaProperty, len(props))
			for key, val := range props {
				if propMap, ok := val.(map[string]any); ok {
					var sp tools.SchemaProperty
					if pt, ok := propMap["type"].(string); ok {
						sp.Type = pt
					}
					if desc, ok := propMap["description"].(string); ok {
						sp.Description = desc
					}
					if def, ok := propMap["default"]; ok {
						sp.Default = def
					}
					if enum, ok := propMap["enum"].([]any); ok {
						for _, e := range enum {
							sp.Enum = append(sp.Enum, fmt.Sprintf("%v", e))
						}
					}
					ts.Properties[key] = sp
				}
			}
		}
		if req, ok := t.schema["required"].([]any); ok {
			for _, r := range req {
				ts.Required = append(ts.Required, fmt.Sprintf("%v", r))
			}
		}
	}

	if ts.Type == "" {
		ts.Type = "object"
	}
	return ts
}

func (t *mcptool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var arguments map[string]any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &arguments); err != nil {
			return nil, fmt.Errorf("parse MCP tool args: %w", err)
		}
	}

	result, err := t.client.CallTool(ctx, t.toolName, arguments)
	if err != nil {
		return nil, fmt.Errorf("MCP tool %s/%s failed: %w", t.serverName, t.toolName, err)
	}

	return parseMCPResult(result)
}

// parseMCPResult parses a tool call result into a simple Go value.
func parseMCPResult(raw json.RawMessage) (any, error) {
	var wrapper struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}

	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return raw, nil // fallback
	}

	var texts []string
	for _, c := range wrapper.Content {
		if c.Type == "text" {
			texts = append(texts, c.Text)
		}
	}

	if wrapper.IsError {
		return map[string]any{"error": texts}, nil
	}

	if len(texts) == 1 {
		return texts[0], nil
	}
	return texts, nil
}
