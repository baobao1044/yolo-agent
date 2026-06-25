package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// Gateway provides HTTP webhook endpoints for the agent.
type Gateway struct {
	port   int
	logger *slog.Logger
	server *http.Server
	handler RequestHandler
}

// RequestHandler processes incoming gateway requests.
type RequestHandler func(ctx context.Context, req *GatewayRequest) (*GatewayResponse, error)

// GatewayRequest is an incoming request to the gateway.
type GatewayRequest struct {
	Source  string                 `json:"source"`  // "webhook", "api", "cli"
	Action  string                 `json:"action"`  // "chat", "schedule", "status"
	Payload map[string]any         `json:"payload"`
	Meta    map[string]string       `json:"meta,omitempty"`
}

// GatewayResponse is the response from the gateway.
type GatewayResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// NewGateway creates a new HTTP gateway.
func NewGateway(port int, handler RequestHandler, logger *slog.Logger) *Gateway {
	if logger == nil {
		logger = slog.Default()
	}
	return &Gateway{
		port:    port,
		handler: handler,
		logger:  logger,
	}
}

// Start starts the HTTP gateway server.
func (g *Gateway) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"ok"}`)
	})

	// Chat endpoint
	mux.HandleFunc("/chat", g.handleChat)

	// Webhook endpoint
	mux.HandleFunc("/webhook", g.handleWebhook)

	// Status endpoint
	mux.HandleFunc("/status", g.handleStatus)

	g.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", g.port),
		Handler: mux,
	}

	g.logger.Info("gateway starting", "port", g.port)

	go func() {
		if err := g.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			g.logger.Error("gateway server error", "error", err)
		}
	}()

	return nil
}

// Stop stops the HTTP gateway server.
func (g *Gateway) Stop(ctx context.Context) error {
	if g.server != nil {
		return g.server.Shutdown(ctx)
	}
	return nil
}

// handleChat handles POST /chat requests.
func (g *Gateway) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body error", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req GatewayRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	req.Source = "api"
	req.Action = "chat"

	resp, err := g.handler(r.Context(), &req)
	if err != nil {
		g.logger.Error("chat handler error", "error", err)
		writeJSON(w, http.StatusInternalServerError, &GatewayResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleWebhook handles POST /webhook requests.
func (g *Gateway) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body error", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req GatewayRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	req.Source = "webhook"

	resp, err := g.handler(r.Context(), &req)
	if err != nil {
		g.logger.Error("webhook handler error", "error", err)
		writeJSON(w, http.StatusInternalServerError, &GatewayResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleStatus handles GET /status requests.
func (g *Gateway) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, &GatewayResponse{
		Success: true,
		Message: "YOLO Agent is running",
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
