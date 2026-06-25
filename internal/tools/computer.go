package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/baobg/yolo-agent/internal/computeruse"
)

// ComputerTool provides desktop automation via MCP.
type ComputerTool struct {
	controller *computeruse.DesktopController
}

// NewComputerTool creates a new ComputerTool.
func NewComputerTool(controller *computeruse.DesktopController) *ComputerTool {
	return &ComputerTool{controller: controller}
}

func (t *ComputerTool) Name() string {
	return "computer"
}

func (t *ComputerTool) Description() string {
	return `Control the desktop: click, type, screenshot, scroll, focus apps, and query accessibility tree. Actions: "click", "type", "screenshot", "scroll", "focus", "accessibility".`
}

func (t *ComputerTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"action": {
				Type:        "string",
				Description: `Desktop action: "click", "type", "screenshot", "scroll", "focus", "accessibility"`,
				Enum:        []string{"click", "type", "screenshot", "scroll", "focus", "accessibility"},
			},
			"x": {
				Type:        "number",
				Description: "X coordinate (for click, scroll).",
			},
			"y": {
				Type:        "number",
				Description: "Y coordinate (for click, scroll).",
			},
			"button": {
				Type:        "string",
				Description: `Mouse button: "left", "right", "middle" (default "left").`,
				Default:     "left",
			},
			"text": {
				Type:        "string",
				Description: "Text to type (for type action).",
			},
			"dx": {
				Type:        "number",
				Description: "Horizontal scroll amount.",
			},
			"dy": {
				Type:        "number",
				Description: "Vertical scroll amount.",
			},
			"app_name": {
				Type:        "string",
				Description: "Application name to focus (for focus action).",
			},
		},
		Required: []string{"action"},
	}
}

// ComputerArgs is the parsed arguments for the computer tool.
type ComputerArgs struct {
	Action  string `json:"action"`
	X       int    `json:"x,omitempty"`
	Y       int    `json:"y,omitempty"`
	Button  string `json:"button,omitempty"`
	Text    string `json:"text,omitempty"`
	Dx      int    `json:"dx,omitempty"`
	Dy      int    `json:"dy,omitempty"`
	AppName string `json:"app_name,omitempty"`
}

func (t *ComputerTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var parsed ComputerArgs
	if err := json.Unmarshal(args, &parsed); err != nil {
		return nil, err
	}

	switch parsed.Action {
	case "click":
		button := parsed.Button
		if button == "" {
			button = "left"
		}
		return t.controller.Click(ctx, parsed.X, parsed.Y, button)

	case "type":
		if parsed.Text == "" {
			return nil, fmt.Errorf("type action requires text")
		}
		return t.controller.Type(ctx, parsed.Text)

	case "screenshot":
		return t.controller.Screenshot(ctx)

	case "scroll":
		return t.controller.Scroll(ctx, parsed.X, parsed.Y, parsed.Dx, parsed.Dy)

	case "focus":
		if parsed.AppName == "" {
			return nil, fmt.Errorf("focus action requires app_name")
		}
		return t.controller.FocusApp(ctx, parsed.AppName)

	case "accessibility":
		return t.controller.AccessibilitySnapshot(ctx)

	default:
		return nil, fmt.Errorf("unknown computer action: %s", parsed.Action)
	}
}
