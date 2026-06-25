# Human-in-the-Loop Approvals

YOLO Agent pauses and asks for approval before executing risky actions.

## Risk Levels

| Level | Description | Examples |
|---|---|---|
| `low` | Safe actions | respond, read-only commands |
| `medium` | Potentially impactful | terminal, browser, MCP tools |
| `high` | Dangerous or destructive | `rm -rf`, `mkfs`, registry edits |

## How it Works

1. The agent picks a tool call.
2. The approval policy evaluates the action.
3. If risk is medium/high:
   - An `ActionRequest` is created.
   - If an interactive UI is available, user is prompted.
   - Otherwise, the action is rejected by default.
4. Approved actions execute; rejected actions return an error to the LLM.

## Configuration

```yaml
approval:
  auto_approve_low_risk: true
  require_approval_for:
    - terminal
    - computer
    - mcp_
  deny_patterns:
    - '(?i)rm\s+-rf\s*/'
    - '(?i)mkfs'
    - '(?i)dd\s+if=.*of=/dev/'
    - '(?i)format\s*[C-Z]:'
    - '(?i)reg\s+delete\s+HKLM'
```

## TUI Approval

When using the TUI mode, risky actions should open an approval modal (planned enhancement).

## Non-Interactive Mode

In `--one-shot` mode, approvals default to **reject**. To run non-interactive risky commands, configure permissive policies carefully or implement a stored approval flow.
