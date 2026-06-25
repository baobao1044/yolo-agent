package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/baobg/yolo-agent/internal/browser"
)

// BrowserTool provides browser automation capabilities.
type BrowserTool struct {
	browser *browser.Browser
}

// NewBrowserTool creates a new BrowserTool.
func NewBrowserTool(b *browser.Browser) *BrowserTool {
	return &BrowserTool{browser: b}
}

func (t *BrowserTool) Name() string {
	return "browser"
}

func (t *BrowserTool) Description() string {
	return `Automate web browser interactions. Actions: "navigate" (go to URL), "screenshot" (capture page), "click" (click element), "type" (enter text), "extract" (get text from element), "evaluate" (run JavaScript).`
}

func (t *BrowserTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"action": {
				Type:        "string",
				Description: `The browser action to perform: "navigate", "screenshot", "click", "type", "extract", "evaluate"`,
				Enum:        []string{"navigate", "screenshot", "click", "type", "extract", "evaluate"},
			},
			"url": {
				Type:        "string",
				Description: "URL to navigate to (for navigate action).",
			},
			"selector": {
				Type:        "string",
				Description: "CSS selector for the target element (for click, type, extract actions).",
			},
			"text": {
				Type:        "string",
				Description: "Text to type into an element (for type action).",
			},
			"expression": {
				Type:        "string",
				Description: "JavaScript expression to evaluate (for evaluate action).",
			},
		},
		Required: []string{"action"},
	}
}

// BrowserArgs is the parsed arguments for the browser tool.
type BrowserArgs struct {
	Action    string `json:"action"`
	URL       string `json:"url,omitempty"`
	Selector  string `json:"selector,omitempty"`
	Text      string `json:"text,omitempty"`
	Expression string `json:"expression,omitempty"`
}

func (t *BrowserTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var parsed BrowserArgs
	if err := json.Unmarshal(args, &parsed); err != nil {
		return nil, err
	}

	switch parsed.Action {
	case "navigate":
		if parsed.URL == "" {
			return nil, fmt.Errorf("navigate action requires url")
		}
		return t.browser.Navigate(ctx, parsed.URL)

	case "screenshot":
		return t.browser.Screenshot(ctx)

	case "click":
		if parsed.Selector == "" {
			return nil, fmt.Errorf("click action requires selector")
		}
		return t.browser.Click(ctx, parsed.Selector)

	case "type":
		if parsed.Selector == "" || parsed.Text == "" {
			return nil, fmt.Errorf("type action requires selector and text")
		}
		return t.browser.Type(ctx, parsed.Selector, parsed.Text)

	case "extract":
		if parsed.Selector == "" {
			return nil, fmt.Errorf("extract action requires selector")
		}
		return t.browser.ExtractText(ctx, parsed.Selector)

	case "evaluate":
		if parsed.Expression == "" {
			return nil, fmt.Errorf("evaluate action requires expression")
		}
		return t.browser.EvaluateJS(ctx, parsed.Expression)

	default:
		return nil, fmt.Errorf("unknown browser action: %s", parsed.Action)
	}
}
