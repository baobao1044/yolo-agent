# Workflows

YOLO Agent includes an Ultracode-inspired workflow engine for complex multi-agent tasks.

## When to Use Workflows

Use the `orchestrate` tool when a task is:

- Large enough to split into independent sub-tasks
- Requires parallel exploration
- Benefits from adversarial verification

## Orchestration Plan

The LLM generates a JSON plan:

```json
{
  "phases": [
    {
      "name": "understand",
      "parallel": true,
      "tasks": [
        {"id": "t1", "prompt": "List files", "tools": ["terminal"]},
        {"id": "t2", "prompt": "Read README", "tools": ["terminal"]}
      ]
    },
    {
      "name": "change",
      "depends_on": ["understand"],
      "parallel": true,
      "tasks": [
        {"id": "t3", "prompt": "Refactor main.go", "tools": ["terminal"]}
      ]
    },
    {
      "name": "verify",
      "depends_on": ["change"],
      "adversarial": true,
      "tasks": [
        {"id": "t4", "prompt": "Check for errors", "tools": ["terminal"]}
      ]
    }
  ]
}
```

## Execution Model

### Phases run sequentially

Each phase executes after its dependencies complete.

### Tasks within a phase run in parallel

- Limited to `max_concurrent` (default 16)
- Each task spawns an independent agent
- Results stored in script variables, not main context

### Adversarial verify

- Verify agents only see original state + changes
- They do NOT see change agents' reasoning
- If issues found, the change phase may re-run

## Runtime Limits

| Limit | Default |
|---|---|
| Max concurrent subagents | 16 |
| Max total subagents per run | 1000 |
| Verify retries | 2 |

These are configurable:

```yaml
workflow:
  max_concurrent: 16
  max_total: 1000
  verify_retries: 2
```
