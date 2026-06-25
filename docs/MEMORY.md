# Memory & RAG

YOLO Agent uses a hybrid memory system.

## Components

### 1. Conversation Store (`memory.Store`)

- SQLite-backed message history
- FTS5 full-text search
- Tool call audit log

### 2. Vector Store (`memory.VectorStore`)

- Stores long-term memories with optional embeddings
- Recall uses cosine similarity + recency + importance
- Pure Go implementation over SQLite (no CGO)

### 3. Profile Store (`memory.ProfileStore`)

- Extracts user facts and preferences from messages
- Builds a `UserProfile` object
- Injects profile summary into system prompt

## Memory Scoring

When recalling memories, each candidate is scored as:

```
score = 0.6 * cosine_similarity(query, memory)
      + 0.25 * recency_decay
      + 0.15 * importance
```

Top-k memories are injected into the context.

## Memory Scope

| Scope | Description |
|---|---|
| `user_default` | Global user memory |
| `conversation_id` | Per-conversation memory |
| `session` | Temporary session memory |

## Extracting Facts

The agent attempts to extract facts from every user message. Examples:

- "My name is Alice" → `source: name`
- "I live in Hanoi" → `source: fact`
- "I like dark theme" → `source: preference`

In production, replace the rule-based extractor with an LLM call using:

```go
profileStore.LLMExtractionPrompt(message)
```

## Configuration

```yaml
# No special config needed; memory uses SQLite automatically
memory:
  driver: sqlite
  database: ~/.yolo-agent/yolo-agent.db
```

## Future Improvements

- Replace brute-force vector search with `sqlite-vec` or `pgvector`
- Add embeddings via OpenAI/Ollama embedding APIs
- Memory consolidation and deduplication
