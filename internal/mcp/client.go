package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"sync"
)

// Client is an MCP (Model Context Protocol) client.
// It connects to MCP servers over stdio to access tools like desktop control.
type Client struct {
	serverCommand string
	serverArgs    []string
	env           map[string]string
	cmd           *exec.Cmd
	stdin         io.WriteCloser
	stdout        *bufio.Reader
	stderr        io.ReadCloser
	logger        *slog.Logger
	initialized   bool
	requestID     int
	mu            sync.Mutex
}

// NewClient creates a new MCP client.
func NewClient(serverCommand string, serverArgs []string, env map[string]string, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	if env == nil {
		env = map[string]string{}
	}
	return &Client{
		serverCommand: serverCommand,
		serverArgs:    serverArgs,
		env:           env,
		logger:        logger,
		requestID:     1,
	}
}

// Connect starts the MCP server process and establishes communication.
func (c *Client) Connect(ctx context.Context) error {
	c.cmd = exec.CommandContext(ctx, c.serverCommand, c.serverArgs...)

	// Apply environment variables
	for k, v := range c.env {
		c.cmd.Env = append(c.cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	stdinPipe, err := c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("create stdin pipe: %w", err)
	}
	c.stdin = stdinPipe

	stdoutPipe, err := c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create stdout pipe: %w", err)
	}
	c.stdout = bufio.NewReader(stdoutPipe)

	stderrPipe, err := c.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("create stderr pipe: %w", err)
	}
	c.stderr = stderrPipe

	// Log stderr
	go func() {
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			c.logger.Debug("MCP server stderr", "line", scanner.Text())
		}
	}()

	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("start MCP server: %w", err)
	}

	// Send initialize request
	if err := c.initialize(); err != nil {
		c.cmd.Process.Kill()
		return fmt.Errorf("initialize MCP: %w", err)
	}

	c.initialized = true
	c.logger.Info("MCP client connected", "server", c.serverCommand)
	return nil
}

// Close shuts down the MCP connection.
func (c *Client) Close() error {
	if c.cmd != nil && c.cmd.Process != nil {
		return c.cmd.Process.Kill()
	}
	return nil
}

// initialize sends the MCP initialize handshake.
func (c *Client) initialize() error {
	initReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID(),
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"clientInfo": map[string]any{
				"name":    "yolo-agent",
				"version": "0.1.0",
			},
		},
	}

	resp, err := c.sendRequest(initReq)
	if err != nil {
		return fmt.Errorf("initialize request: %w", err)
	}

	c.logger.Debug("MCP initialize response", "response", string(resp))

	// Send initialized notification
	_ = c.sendNotification("notifications/initialized", map[string]any{})

	return nil
}

// CallTool calls a tool on the MCP server.
func (c *Client) CallTool(ctx context.Context, toolName string, arguments map[string]any) (json.RawMessage, error) {
	if !c.initialized {
		return nil, fmt.Errorf("MCP client not initialized")
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID(),
		"method":  "tools/call",
		"params": map[string]any{
			"name":      toolName,
			"arguments": arguments,
		},
	}

	return c.sendRequest(req)
}

// ListTools lists available tools on the MCP server.
func (c *Client) ListTools(ctx context.Context) ([]MCPTool, error) {
	if !c.initialized {
		return nil, fmt.Errorf("MCP client not initialized")
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID(),
		"method":  "tools/list",
		"params":  map[string]any{},
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}

	var result struct {
		Tools []MCPTool `json:"tools"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("parse tools list: %w", err)
	}

	return result.Tools, nil
}

// sendRequest sends a JSON-RPC request and returns the result.
func (c *Client) sendRequest(req map[string]any) (json.RawMessage, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	c.logger.Debug("MCP request", "request", string(data))

	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}
	if _, err := c.stdin.Write([]byte{'\n'}); err != nil {
		return nil, fmt.Errorf("write request newline: %w", err)
	}

	line, err := c.stdout.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	c.logger.Debug("MCP response", "response", string(line))

	var response struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      any             `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *MCPError       `json:"error"`
	}

	if err := json.Unmarshal(line, &response); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if response.Error != nil {
		return nil, fmt.Errorf("MCP error %d: %s", response.Error.Code, response.Error.Message)
	}

	return response.Result, nil
}

// sendNotification sends a JSON-RPC notification (no response expected).
func (c *Client) sendNotification(method string, params map[string]any) error {
	req := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	if _, werr := c.stdin.Write(append(data, '\n')); werr != nil {
		return fmt.Errorf("write notification: %w", werr)
	}
	_, err = c.stdin.Write([]byte{'\n'})
	return err
}

// nextID returns the next request ID.
func (c *Client) nextID() int {
	id := c.requestID
	c.requestID++
	return id
}

// MCPTool represents an MCP tool.
type MCPTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// MCPError represents a JSON-RPC error.
type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}
