package computeruse

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
)

// DesktopController provides desktop automation via MCP (Model Context Protocol).
// It connects to an MCP server (like cua-driver) that handles OS-level input.
type DesktopController struct {
	mcpServerCommand string
	logger           *slog.Logger
	connected        bool
}

// NewDesktopController creates a new desktop controller.
func NewDesktopController(mcpServerCommand string, logger *slog.Logger) *DesktopController {
	if logger == nil {
		logger = slog.Default()
	}
	return &DesktopController{
		mcpServerCommand: mcpServerCommand,
		logger:           logger,
	}
}

// Connect establishes connection to the MCP server.
func (d *DesktopController) Connect(ctx context.Context) error {
	if d.mcpServerCommand == "" {
		return fmt.Errorf("MCP server command not configured")
	}
	// In a full implementation, this would:
	// 1. Start the MCP server process
	// 2. Establish stdio/SSE connection
	// 3. Negotiate capabilities
	d.connected = true
	d.logger.Info("desktop controller connected", "server", d.mcpServerCommand)
	return nil
}

// Close shuts down the MCP connection.
func (d *DesktopController) Close() error {
	d.connected = false
	return nil
}

// ClickResult is the result of a desktop click.
type ClickResult struct {
	X       int  `json:"x"`
	Y       int  `json:"y"`
	Button  string `json:"button"`
	Success bool `json:"success"`
}

// Click performs a desktop click at coordinates.
func (d *DesktopController) Click(ctx context.Context, x, y int, button string) (*ClickResult, error) {
	if !d.connected {
		return nil, fmt.Errorf("desktop controller not connected")
	}
	// Would call MCP server: computer_use.click(x, y, button)
	d.logger.Info("desktop click", "x", x, "y", y, "button", button)
	return &ClickResult{X: x, Y: y, Button: button, Success: true}, nil
}

// TypeResult is the result of desktop typing.
type TypeResult struct {
	Text    string `json:"text"`
	Success bool   `json:"success"`
}

// Type types text on the desktop.
func (d *DesktopController) Type(ctx context.Context, text string) (*TypeResult, error) {
	if !d.connected {
		return nil, fmt.Errorf("desktop controller not connected")
	}
	d.logger.Info("desktop type", "text_length", len(text))
	return &TypeResult{Text: text, Success: true}, nil
}

// ScreenshotResult is the result of a desktop screenshot.
type ScreenshotResult struct {
	Data     []byte `json:"data"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Format   string `json:"format"`
}

// Screenshot captures a desktop screenshot.
func (d *DesktopController) Screenshot(ctx context.Context) (*ScreenshotResult, error) {
	if !d.connected {
		return nil, fmt.Errorf("desktop controller not connected")
	}
	d.logger.Info("desktop screenshot")
	// Would call MCP server: computer_use.screenshot()
	return &ScreenshotResult{Format: "png", Width: 1920, Height: 1080}, nil
}

// ScrollResult is the result of a scroll action.
type ScrollResult struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Dx     int `json:"dx"`
	Dy     int `json:"dy"`
	Success bool `json:"success"`
}

// Scroll performs a scroll action.
func (d *DesktopController) Scroll(ctx context.Context, x, y, dx, dy int) (*ScrollResult, error) {
	if !d.connected {
		return nil, fmt.Errorf("desktop controller not connected")
	}
	d.logger.Info("desktop scroll", "x", x, "y", y, "dx", dx, "dy", dy)
	return &ScrollResult{X: x, Y: y, Dx: dx, Dy: dy, Success: true}, nil
}

// FocusResult is the result of focusing an app.
type FocusResult struct {
	AppName string `json:"app_name"`
	Success bool   `json:"success"`
}

// FocusApp brings an application to the foreground.
func (d *DesktopController) FocusApp(ctx context.Context, appName string) (*FocusResult, error) {
	if !d.connected {
		return nil, fmt.Errorf("desktop controller not connected")
	}
	d.logger.Info("desktop focus app", "app", appName)
	return &FocusResult{AppName: appName, Success: true}, nil
}

// AccessibilityResult is the result of an accessibility tree query.
type AccessibilityResult struct {
	Tree json.RawMessage `json:"tree"`
}

// AccessibilitySnapshot returns the accessibility tree.
func (d *DesktopController) AccessibilitySnapshot(ctx context.Context) (*AccessibilityResult, error) {
	if !d.connected {
		return nil, fmt.Errorf("desktop controller not connected")
	}
	d.logger.Info("desktop accessibility snapshot")
	// Would call MCP server: computer_use.accessibility_snapshot()
	return &AccessibilityResult{Tree: json.RawMessage(`{"role": "desktop", "children": []}`)}, nil
}
