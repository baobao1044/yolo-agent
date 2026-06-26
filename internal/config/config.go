package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/baobg/yolo-agent/internal/approvals"
	"github.com/baobg/yolo-agent/internal/corerag"
	"github.com/baobg/yolo-agent/internal/gateway"
	"github.com/baobg/yolo-agent/internal/mcp"
	"gopkg.in/yaml.v3"
)

// Config holds all configuration for the YOLO Agent.
type Config struct {
	LLM     LLMConfig     `yaml:"llm"`
	Agent   AgentConfig   `yaml:"agent"`
	Memory  MemoryConfig  `yaml:"memory"`
	Tools   ToolsConfig   `yaml:"tools"`
	Workflow WorkflowConfig        `yaml:"workflow"`
	UI       UIConfig             `yaml:"ui"`
	Gateway  GatewayConfig        `yaml:"gateway"`
	McpServers []mcp.ServerConfig  `yaml:"mcp_servers"`
	Telegram gateway.TelegramConfig `yaml:"telegram"`
	Discord  gateway.DiscordConfig  `yaml:"discord"`
	Slack    gateway.SlackConfig    `yaml:"slack"`
	Email    gateway.EmailConfig    `yaml:"email"`
	Approval approvals.Policy       `yaml:"approval"`
	CoreRAG  corerag.CoreRAGConfig  `yaml:"corerag"`
}

// LLMConfig configures the LLM provider.
type LLMConfig struct {
	Provider    string  `yaml:"provider"`    // "openai-compatible" (default)
	BaseURL     string  `yaml:"base_url"`    // OpenRouter/OpenAI endpoint
	APIKey      string  `yaml:"api_key"`     // loaded from env if empty
	Model       string  `yaml:"model"`       // model name e.g. "anthropic/claude-3.5-sonnet"
	MaxTokens   int     `yaml:"max_tokens"` // max response tokens
	Temperature float64 `yaml:"temperature"`
}

// AgentConfig configures the agent loop.
type AgentConfig struct {
	MaxIterations int    `yaml:"max_iterations"` // max tool-call iterations per turn
	SystemPrompt  string `yaml:"system_prompt"`  // default system prompt
	DataDir       string `yaml:"data_dir"`       // ~/.yolo-agent
}

// MemoryConfig configures persistence.
type MemoryConfig struct {
	Driver    string `yaml:"driver"`     // "sqlite" (default)
	Database  string `yaml:"database"`   // path to SQLite file
	FTSEnabled bool  `yaml:"fts_enabled"`
}

// ToolsConfig configures available tools.
type ToolsConfig struct {
	TerminalEnabled   bool     `yaml:"terminal_enabled"`
	BrowserEnabled    bool     `yaml:"browser_enabled"`
	ComputerEnabled   bool     `yaml:"computer_enabled"`
	TerminalAllowList []string `yaml:"terminal_allow_list"`
	TerminalDenyList  []string `yaml:"terminal_deny_list"`
}

// WorkflowConfig configures the workflow engine.
type WorkflowConfig struct {
	Enabled        bool `yaml:"enabled"`
	MaxConcurrent  int  `yaml:"max_concurrent"`  // max concurrent subagents (default 16)
	MaxTotal       int  `yaml:"max_total"`        // max total subagents per run (default 1000)
	VerifyRetries  int  `yaml:"verify_retries"`   // max adversarial verify retries (default 2)
}

// UIConfig configures the TUI.
type UIConfig struct {
	Theme  string `yaml:"theme"`  // "dark" (default) or "light"
	Height int    `yaml:"height"` // TUI height (0 = auto)
	Width  int    `yaml:"width"`  // TUI width (0 = auto)
}

// GatewayConfig configures the gateway.
type GatewayConfig struct {
	Enabled bool `yaml:"enabled"`
	Port    int  `yaml:"port"`
}

// DefaultConfig returns a config with sensible defaults.
func DefaultConfig() *Config {
	homeDir, _ := os.UserHomeDir()
	dataDir := filepath.Join(homeDir, ".yolo-agent")

	return &Config{
		LLM: LLMConfig{
			Provider:    "openai-compatible",
			BaseURL:     "https://openrouter.ai/api/v1",
			APIKey:      "", // must be set via env YOLO_API_KEY or config
			Model:       "anthropic/claude-3.5-sonnet",
			MaxTokens:   4096,
			Temperature: 0.7,
		},
		Agent: AgentConfig{
			MaxIterations: 50,
			SystemPrompt:  "You are YOLO Agent, an autonomous AI assistant with computer-use capabilities. You can execute terminal commands, browse the web, control the desktop, and orchestrate multi-agent workflows. Always be helpful, precise, and safe.",
			DataDir:       dataDir,
		},
		Memory: MemoryConfig{
			Driver:     "sqlite",
			Database:   filepath.Join(dataDir, "yolo-agent.db"),
			FTSEnabled: true,
		},
		Tools: ToolsConfig{
			TerminalEnabled: true,
			BrowserEnabled:  true,
			ComputerEnabled: true,
		},
		Workflow: WorkflowConfig{
			Enabled:       true,
			MaxConcurrent: 16,
			MaxTotal:      1000,
			VerifyRetries: 2,
		},
		UI: UIConfig{
			Theme: "dark",
		},
		Gateway: GatewayConfig{
			Enabled: false,
			Port:    0,
		},
		Approval: *approvals.DefaultPolicy(),
		CoreRAG:  corerag.DefaultCoreRAG(),
	}
}

// Load reads config from a YAML file, falling back to defaults.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Build compiled patterns in approval policy
	if err := cfg.Approval.Build(); err != nil {
		return nil, fmt.Errorf("approval policy: %w", err)
	}

	// Override API key from environment if set
	if envKey := os.Getenv("YOLO_API_KEY"); envKey != "" {
		cfg.LLM.APIKey = envKey
	}

	return cfg, nil
}

// Save writes the config to a YAML file.
func (c *Config) Save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}
