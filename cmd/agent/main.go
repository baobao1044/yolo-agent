package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/baobg/yolo-agent/internal/agent"
	"github.com/baobg/yolo-agent/internal/approvals"
	"github.com/baobg/yolo-agent/internal/background"
	"github.com/baobg/yolo-agent/internal/browser"
	"github.com/baobg/yolo-agent/internal/computeruse"
	"github.com/baobg/yolo-agent/internal/config"
	"github.com/baobg/yolo-agent/internal/corerag"
	"github.com/baobg/yolo-agent/internal/embeddings"
	"github.com/baobg/yolo-agent/internal/gateway"
	"github.com/baobg/yolo-agent/internal/llm"
	"github.com/baobg/yolo-agent/internal/memory"
	"github.com/baobg/yolo-agent/internal/mcp"
	"github.com/baobg/yolo-agent/internal/skills"
	"github.com/baobg/yolo-agent/internal/tools"
	"github.com/baobg/yolo-agent/internal/ui"
	"github.com/baobg/yolo-agent/internal/workflow"
)

func main() {
	var (
		cfgPath = flag.String("config", "", "path to config file (default: ~/.yolo-agent/config.yaml)")
		help    = flag.Bool("help", false, "show help")
		version = flag.Bool("version", false, "show version")
		initCfg = flag.Bool("init", false, "create default config file and exit")
		oneShot = flag.String("one-shot", "", "run a single message and print the response (no TUI)")
	)
	flag.Parse()

	if *help {
		flag.Usage()
		os.Exit(0)
	}

	if *version {
		fmt.Println("YOLO Agent v0.2.0")
		os.Exit(0)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	finalCfgPath := *cfgPath
	if finalCfgPath == "" {
		finalCfgPath = os.Getenv("YOLO_CONFIG")
	}
	if finalCfgPath == "" {
		homeDir, _ := os.UserHomeDir()
		finalCfgPath = fmt.Sprintf("%s/.yolo-agent/config.yaml", homeDir)
	}

	cfg := config.DefaultConfig()

	if *initCfg {
		if err := cfg.Save(finalCfgPath); err != nil {
			logger.Error("failed to create config", "error", err)
			os.Exit(1)
		}
		fmt.Printf("Created default config at: %s\n", finalCfgPath)
		fmt.Println("Edit the llm.api_key field and run again.")
		os.Exit(0)
	}

	if loadedCfg, err := config.Load(finalCfgPath); err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	} else {
		cfg = loadedCfg
	}

	if cfg.LLM.APIKey == "" {
		logger.Error("LLM API key is required. Set YOLO_API_KEY or configure llm.api_key, or run with --init")
		os.Exit(1)
	}

	store, err := memory.NewStore(cfg.Memory.Database)
	if err != nil {
		logger.Error("failed to open memory store", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	vectorStore, err := memory.NewVectorStore(store.DB())
	if err != nil {
		logger.Error("failed to init vector store", "error", err)
		os.Exit(1)
	}

	profileStore := memory.NewProfileStore(vectorStore)

	// Background task store and runner.
	bgStore, err := background.NewStore(store.DB())
	if err != nil {
		logger.Error("failed to init background store", "error", err)
		os.Exit(1)
	}
	bgNotifier := &backgroundNotifier{logger: logger}

	// Load skills
	skillLoader := skills.NewLoader("skills")
	if err := skillLoader.LoadAll(); err != nil {
		logger.Warn("failed to load bundled skills", "error", err)
	}
	dataSkillLoader := skills.NewLoader(fmt.Sprintf("%s/skills", cfg.Agent.DataDir))
	if err := dataSkillLoader.LoadAll(); err != nil {
		logger.Warn("failed to load data dir skills", "error", err)
	}
	for name, skill := range dataSkillLoader.All() {
		_ = name
		_ = skill
	}

	systemPrompt := cfg.Agent.SystemPrompt
	for _, skill := range skillLoader.All() {
		systemPrompt += skill.InjectPrompt()
	}

	llmClient := llm.NewOpenAIClient(
		cfg.LLM.BaseURL,
		cfg.LLM.APIKey,
		cfg.LLM.Model,
		cfg.LLM.MaxTokens,
		float32(cfg.LLM.Temperature),
	)

	browserInstance := browser.NewBrowser()
	if err := browserInstance.Start(context.Background()); err != nil {
		logger.Warn("failed to start browser", "error", err)
	}
	defer browserInstance.Close()

	desktopController := computeruse.NewDesktopController("", logger)
	if cfg.Tools.ComputerEnabled && len(cfg.McpServers) > 0 {
		if err := desktopController.Connect(context.Background()); err != nil {
			logger.Warn("failed to connect desktop controller", "error", err)
		}
	}
	defer desktopController.Close()

	registry := tools.NewRegistry()
	registry.MustRegister(tools.NewRespondTool())
	registry.MustRegister(tools.NewTerminalTool(cfg.Tools.TerminalAllowList, cfg.Tools.TerminalDenyList))
	registry.MustRegister(tools.NewBrowserTool(browserInstance))
	registry.MustRegister(tools.NewComputerTool(desktopController))

	// CORE Code-RAG engine: index a repository and retrieve budget-aware
	// context. Initialized lazily and registered only when enabled; failures
	// (e.g. missing ONNX model) fall back softly so the agent still runs.
	var coreragEngine *corerag.Engine
	if cfg.CoreRAG.Enabled {
		if err := cfg.CoreRAG.Validate(); err != nil {
			logger.Warn("invalid corerag config; disabling", "error", err)
		} else {
			crStore, err := corerag.NewStore(store.DB())
			if err != nil {
				logger.Warn("failed to init corerag store; disabling", "error", err)
			} else {
				embedder, err := embeddings.NewEmbedder(cfg.CoreRAG.Embedding, llmClient, logger)
				if err != nil {
					logger.Warn("failed to init corerag embedder; disabling", "error", err)
				} else {
					coreragEngine = corerag.NewEngine(crStore, embedder, cfg.CoreRAG, logger)
					registry.MustRegister(corerag.NewIndexRepoTool(coreragEngine))
					registry.MustRegister(corerag.NewRepoQueryTool(coreragEngine))
					logger.Info("corerag enabled", "embedding", cfg.CoreRAG.Embedding.Provider)
				}
			}
		}
	}

	// Connect MCP servers and register discovered tools
	mcpManager := mcp.NewManager(cfg.McpServers, registry, logger)
	if len(cfg.McpServers) > 0 {
		if err := mcpManager.ConnectAll(context.Background()); err != nil {
			logger.Warn("failed to connect MCP servers", "error", err)
		}
	}
	defer mcpManager.Close()

	orchestrator := agent.NewOrchestrator(registry, agent.OrchestratorConfig{
		MaxConcurrent: cfg.Workflow.MaxConcurrent,
		MaxTotal:      cfg.Workflow.MaxTotal,
		VerifyRetries: cfg.Workflow.VerifyRetries,
	}, logger)
	registry.MustRegister(agent.NewOrchestrateTool(orchestrator))

	approvalStore := approvals.NewMemoryStore()
	approvalPolicy := approvals.DefaultPolicy()
	if len(cfg.Approval.RequireApprovalFor) > 0 || len(cfg.Approval.DenyPatternStrings) > 0 {
		approvalPolicy = &cfg.Approval
		if err := approvalPolicy.Build(); err != nil {
			logger.Error("invalid approval policy", "error", err)
			os.Exit(1)
		}
	}

	// Background runner and scheduler: execute agent turns inside a filtered tool registry.
	bgRunner := background.NewRunner(bgStore, bgNotifier, logger)
	buildRunFunc := func(ctx context.Context, env *background.Envelope) background.RunFunc {
		return func(runCtx context.Context, instruction string) (string, error) {
			filtered := registry.Filter(env.Scope.AllowedTools)
			if env.Scope.AllowAll {
				filtered = registry
			}
			subAgent := agent.NewAIAgent(llmClient, filtered, systemPrompt, cfg.Agent.MaxIterations, logger)
			return runAgentTurn(runCtx, subAgent, env, instruction)
		}
	}
	bgScheduler := background.NewScheduler(bgStore, bgRunner, buildRunFunc, logger)

	registry.MustRegister(tools.NewScheduleTool(bgScheduler))
	registry.MustRegister(tools.NewEnvelopeStateTool(bgStore))

	workflow.DefaultSubagentExecutor = func(ctx context.Context, prompt string, filteredRegistry *tools.Registry) (string, error) {
		subAgent := agent.NewAIAgent(llmClient, filteredRegistry, systemPrompt, cfg.Agent.MaxIterations, logger)
		return subAgent.Run(ctx, prompt)
	}

	mainAgent := agent.NewAIAgent(llmClient, registry, systemPrompt, cfg.Agent.MaxIterations, logger)

	approvalCallback := func(req *approvals.ActionRequest) (*bool, error) {
		logger.Warn("Approval required but not in interactive mode.",
			"id", req.ID,
			"tool", req.ToolName,
			"risk", req.Risk,
			"reason", req.Reason,
		)
		approved := false
		return &approved, nil
	}

	checkpoint := approvals.NewCheckpoint(approvalPolicy, approvalStore, approvalCallback)
	// Wire checkpoint by wrapping tool execution in the future.
	_ = checkpoint

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	// Messaging gateways
	var transports []gateway.Transport
	if cfg.Telegram.Enabled {
		transports = append(transports, gateway.NewTelegramTransport(cfg.Telegram, logger))
	}
	if cfg.Discord.Enabled {
		transports = append(transports, gateway.NewDiscordTransport(cfg.Discord, logger))
	}
	if cfg.Slack.Enabled {
		transports = append(transports, gateway.NewSlackTransport(cfg.Slack, logger))
	}
	if cfg.Email.Enabled {
		transports = append(transports, gateway.NewEmailTransport(cfg.Email, logger))
	}

	multiGateway := gateway.NewMultiTransport(transports, logger)
	messagingCtx, messagingCancel := context.WithCancel(ctx)
	defer messagingCancel()

	go func() {
		err := multiGateway.Start(messagingCtx, func(ctx context.Context, msg *gateway.Envelope) (*gateway.Envelope, error) {
			resp, err := handleAgent(ctx, mainAgent, store, vectorStore, profileStore, msg.SenderID, msg.Text, logger)
			if err != nil {
				logger.Error("gateway handler error", "error", err)
				return nil, err
			}
			return &gateway.Envelope{
				Source: msg.Source,
				ChatID: msg.ChatID,
				Text:   resp,
			}, nil
		})
		if err != nil {
			logger.Error("messaging gateway error", "error", err)
		}
	}()

	// HTTP Gateway
	var httpGateway *gateway.Gateway
	if cfg.Gateway.Port > 0 {
		httpGateway = gateway.NewGateway(cfg.Gateway.Port, func(ctx context.Context, req *gateway.GatewayRequest) (*gateway.GatewayResponse, error) {
			switch req.Action {
			case "chat":
				payload, _ := req.Payload["message"].(string)
				resp, err := handleAgent(ctx, mainAgent, store, vectorStore, profileStore, "default", payload, logger)
				if err != nil {
					return &gateway.GatewayResponse{Success: false, Error: err.Error()}, nil
				}
				return &gateway.GatewayResponse{Success: true, Message: resp}, nil
			case "task_status":
				return handleTaskStatus(bgStore, req.Payload)
			case "task_pause":
				return handleTaskPause(bgRunner, bgStore, req.Payload)
			case "task_resume":
				return handleTaskResume(bgScheduler, req.Payload)
			case "task_kill":
				return handleTaskKill(bgRunner, req.Payload)
			default:
				return &gateway.GatewayResponse{Success: false, Error: "unsupported action"}, nil
			}
		}, logger)

		if err := httpGateway.Start(ctx); err != nil {
			logger.Error("failed to start gateway", "error", err)
			os.Exit(1)
		}
		defer httpGateway.Stop(ctx)
		logger.Info("gateway started", "port", cfg.Gateway.Port)
	}

	// Start the background scheduler after all dependencies exist.
	if err := bgScheduler.Start(ctx); err != nil {
		logger.Error("failed to start background scheduler", "error", err)
		os.Exit(1)
	}
	defer bgScheduler.Stop()
	logger.Info("background scheduler started")

	if *oneShot != "" {
		logger.Info("running one-shot message", "message", *oneShot)
		resp, err := handleAgent(ctx, mainAgent, store, vectorStore, profileStore, "default", *oneShot, logger)
		if err != nil {
			logger.Error("one-shot failed", "error", err)
			os.Exit(1)
		}
		fmt.Println(resp)
		os.Exit(0)
	}

	// Run TUI
	tui := ui.NewTUI(func(input string) (string, error) {
		resp, err := handleAgent(ctx, mainAgent, store, vectorStore, profileStore, "default", input, logger)
		return resp, err
	})

	if err := tui.Run(); err != nil {
		logger.Error("TUI error", "error", err)
		os.Exit(1)
	}
}

// runAgentTurn executes a single agent turn inside an envelope and enforces runtime budgets.
func runAgentTurn(ctx context.Context, subAgent *agent.AIAgent, env *background.Envelope, instruction string) (string, error) {
	if err := env.CheckBudget(0, 1); err != nil {
		return "", err
	}

	resp, err := subAgent.Run(ctx, instruction)
	if err != nil {
		return "", err
	}

	// Approximate usage: one tool/agent run counted against the envelope budget.
	env.RecordUsage(0, 1)
	return resp, nil
}

// handleAgent runs the agent with memory injection and persistence.
func handleAgent(ctx context.Context, mainAgent *agent.AIAgent, store *memory.Store, vectorStore *memory.VectorStore, profileStore *memory.ProfileStore, userID, input string, logger *slog.Logger) (string, error) {
	// Extract and store facts from user message
	facts, err := profileStore.ExtractFacts(ctx, input)
	if err != nil {
		logger.Warn("failed to extract facts", "error", err)
	}
	if err := profileStore.SaveMemories(ctx, facts); err != nil {
		logger.Warn("failed to save profile memories", "error", err)
	}

	// Save user message
	if _, err := store.SaveMessage("default", "user", input); err != nil {
		logger.Warn("failed to save user message", "error", err)
	}

	// Recall relevant memories
	memories, err := vectorStore.Recall(ctx, nil, 5)
	if err != nil {
		logger.Warn("failed to recall memories", "error", err)
	}

	// Inject user profile + memories into the conversation
	var memoryText string
	if profile, err := profileStore.Get(ctx, userID); err == nil && profile != nil {
		memoryText = profile.SummaryText()
	}
	for _, m := range memories {
		if memoryText != "" {
			memoryText += "\n"
		}
		memoryText += "- " + m.Content
	}
	if memoryText != "" {
		mainAgent.AddMessage(agent.Message{
			Role:    agent.RoleSystem,
			Content: "## Relevant context\n" + memoryText,
		})
	}

	resp, err := mainAgent.Run(ctx, input)
	if err != nil {
		if _, saveErr := store.SaveMessage("default", "error", err.Error()); saveErr != nil {
			logger.Warn("failed to save error message", "error", saveErr)
		}
		return "", err
	}

	// Store assistant response as memory
	if _, err := vectorStore.Store(ctx, memory.Memory{
		ConversationID: "default",
		Scope:          "user_default",
		Source:         "assistant",
		Content:        resp,
		Importance:     0.7,
	}); err != nil {
		logger.Warn("failed to store assistant memory", "error", err)
	}

	if _, err := store.SaveMessage("default", "assistant", resp); err != nil {
		logger.Warn("failed to save assistant message", "error", err)
	}
	if err := store.SaveConversation("default", map[string]string{"model": "unknown"}); err != nil {
		logger.Warn("failed to save conversation", "error", err)
	}
	return resp, nil
}

// backgroundNotifier emits background task status updates.
// Today it logs to stderr; messaging transports can be wired here once a default
// target chat ID is configured.
type backgroundNotifier struct {
	logger *slog.Logger
}

// Notify implements background.Notifier.
func (n *backgroundNotifier) Notify(ctx context.Context, envelopeID, title, message string, channels []string) error {
	n.logger.Info("background notification",
		"envelope", envelopeID,
		"title", title,
		"message", message,
		"channels", channels,
	)
	return nil
}

// HTTP task control helpers.

func handleTaskStatus(store *background.Store, payload map[string]any) (*gateway.GatewayResponse, error) {
	id, _ := payload["id"].(string)
	if id != "" {
		env, err := store.Get(id)
		if err != nil {
			return &gateway.GatewayResponse{Success: false, Error: err.Error()}, nil
		}
		return &gateway.GatewayResponse{Success: true, Data: env.Snapshot()}, nil
	}
	envs, err := store.List()
	if err != nil {
		return &gateway.GatewayResponse{Success: false, Error: err.Error()}, nil
	}
	var out []background.Envelope
	for _, env := range envs {
		out = append(out, env.Snapshot())
	}
	return &gateway.GatewayResponse{Success: true, Data: out}, nil
}

func handleTaskPause(runner *background.Runner, store *background.Store, payload map[string]any) (*gateway.GatewayResponse, error) {
	id, ok := payload["id"].(string)
	if !ok || id == "" {
		return &gateway.GatewayResponse{Success: false, Error: "id is required"}, nil
	}
	// Kill any active run, then pause scheduling.
	_ = runner.Kill(id)
	env, err := runner.Pause(id)
	if err != nil {
		return &gateway.GatewayResponse{Success: false, Error: err.Error()}, nil
	}
	return &gateway.GatewayResponse{Success: true, Data: env.Snapshot()}, nil
}

func handleTaskResume(scheduler *background.Scheduler, payload map[string]any) (*gateway.GatewayResponse, error) {
	id, ok := payload["id"].(string)
	if !ok || id == "" {
		return &gateway.GatewayResponse{Success: false, Error: "id is required"}, nil
	}
	env, err := scheduler.Resume(id)
	if err != nil {
		return &gateway.GatewayResponse{Success: false, Error: err.Error()}, nil
	}
	return &gateway.GatewayResponse{Success: true, Data: env.Snapshot()}, nil
}

func handleTaskKill(runner *background.Runner, payload map[string]any) (*gateway.GatewayResponse, error) {
	id, ok := payload["id"].(string)
	if !ok || id == "" {
		return &gateway.GatewayResponse{Success: false, Error: "id is required"}, nil
	}
	if err := runner.Kill(id); err != nil {
		return &gateway.GatewayResponse{Success: false, Error: err.Error()}, nil
	}
	return &gateway.GatewayResponse{Success: true, Message: "task killed"}, nil
}
