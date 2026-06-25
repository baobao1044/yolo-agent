package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Registry manages all available tools.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry creates a new tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry.
func (r *Registry) Register(tool Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := tool.Name()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool %q already registered", name)
	}
	r.tools[name] = tool
	return nil
}

// MustRegister registers a tool, panicking on error.
func (r *Registry) MustRegister(tool Tool) {
	if err := r.Register(tool); err != nil {
		panic(err)
	}
}

// Get retrieves a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// List returns all registered tool names.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// Definitions returns all tool definitions in OpenAI-compatible format.
func (r *Registry) Definitions() []ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defs := make([]ToolDefinition, 0, len(r.tools))
	for _, tool := range r.tools {
		defs = append(defs, ToDefinition(tool))
	}
	return defs
}

// Execute dispatches a tool call to the appropriate tool.
func (r *Registry) Execute(ctx context.Context, name string, args json.RawMessage) (any, error) {
	r.mu.RLock()
	tool, ok := r.tools[name]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("tool %q not found", name)
	}

	return tool.Execute(ctx, args)
}

// ExecuteWithTimeout dispatches a tool call with a default timeout.
func (r *Registry) ExecuteWithTimeout(ctx context.Context, name string, args json.RawMessage) (any, error) {
	return r.Execute(ctx, name, args)
}

// Filter returns a new registry containing only tools with the given names.
func (r *Registry) Filter(allowed []string) *Registry {
	filtered := NewRegistry()
	allowedSet := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		allowedSet[name] = true
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	for name, tool := range r.tools {
		if allowedSet[name] {
			filtered.tools[name] = tool
		}
	}
	return filtered
}

// Size returns the number of registered tools.
func (r *Registry) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}
