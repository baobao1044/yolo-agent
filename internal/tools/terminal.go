package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

// TerminalTool executes shell/terminal commands.
type TerminalTool struct {
	allowList []string
	denyList  []string
	timeout   time.Duration
}

// NewTerminalTool creates a new TerminalTool.
func NewTerminalTool(allowList, denyList []string) *TerminalTool {
	return &TerminalTool{
		allowList: allowList,
		denyList:  denyList,
		timeout:   120 * time.Second,
	}
}

func (t *TerminalTool) Name() string {
	return "terminal"
}

func (t *TerminalTool) Description() string {
	return "Execute a shell command and return its output. Use this to run terminal commands, scripts, and system utilities."
}

func (t *TerminalTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"command": {
				Type:        "string",
				Description: "The command to execute.",
			},
			"timeout": {
				Type:        "number",
				Description: "Timeout in seconds (default 120).",
				Default:     120,
			},
			"working_dir": {
				Type:        "string",
				Description: "Working directory for the command (optional).",
			},
		},
		Required: []string{"command"},
	}
}

// TerminalArgs is the parsed arguments for the terminal tool.
type TerminalArgs struct {
	Command    string  `json:"command"`
	Timeout    float64 `json:"timeout"`
	WorkingDir string  `json:"working_dir"`
}

// TerminalResult is the result of a terminal command execution.
type TerminalResult struct {
	Command  string `json:"command"`
	Output   string `json:"output"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error,omitempty"`
}

func (t *TerminalTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var parsed TerminalArgs
	if err := json.Unmarshal(args, &parsed); err != nil {
		return nil, err
	}

	// Check deny list
	for _, denied := range t.denyList {
		if parsed.Command == denied {
			return TerminalResult{
				Command: parsed.Command,
				Error:   fmt.Sprintf("command %q is in the deny list", parsed.Command),
			}, nil
		}
	}

	// Determine shell
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", parsed.Command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", parsed.Command)
	}

	if parsed.WorkingDir != "" {
		cmd.Dir = parsed.WorkingDir
	}

	// Set timeout
	timeout := t.timeout
	if parsed.Timeout > 0 {
		timeout = time.Duration(parsed.Timeout * float64(time.Second))
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd = exec.CommandContext(timeoutCtx, cmd.Args[0], cmd.Args[1:]...)
	if parsed.WorkingDir != "" {
		cmd.Dir = parsed.WorkingDir
	}

	output, err := cmd.CombinedOutput()

	result := TerminalResult{
		Command: parsed.Command,
		Output:  string(output),
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		}
		result.Error = err.Error()
	}

	return result, nil
}
