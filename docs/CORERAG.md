# CORE Code-RAG

YOLO Agent's **Code-RAG** engine implements the CORE framework — *Budget-Aware Representation Selection for Repository-Scale Code Reasoning*. It indexes a codebase into a structure graph, classifies symbols by architectural role, and at query time assembles a token-budgeted context by choosing graduated representation levels per symbol, framed as a Multi-Choice Knapsack Problem (MCKP).

This document describes the design, algorithms, and current limitations. The implementation lives in [`internal/corerag`](../internal/corerag) with embedding backends in [`internal/embeddings`](../internal/embeddings).

---

## Why budget-aware retrieval?

Naive code RAG retrieves whole files (or fixed-size chunks) and concatenates them into the prompt. This works for a snippet but breaks at repository scale: a 50k-LOC repo cannot fit in any context window, and a flat file dump wastes the budget on boilerplate while starving the symbols that actually matter.

CORE's insight is to treat context assembly as an **optimization problem**. Each symbol can be rendered at several compression levels, each with a *token cost* and an *information-retention value*. Given a token budget `B`, the engine picks the per-symbol representation levels that maximize total retained information without exceeding `B` — a Multi-Choice Knapsack Problem it solves greedily in `O(|V| log |V|)`.

---

## The pipeline

```
Query (natural language)
  │
  ▼
1. Semantic rank        cosine similarity between the query embedding and
   (embeddings)         each symbol's retrieval embedding → top-K seeds
  │
  ▼
2. BFS graph expansion  from the seeds, walk the call/import graph up to
   (S(v) ≥ θ)           depth k, keeping only nodes whose relevance S(v)
                        clears the threshold θ
  │
  ▼
3. Core / peripheral    seeds → core (V₀, pinned at raw source)
   split                expanded neighbors → peripheral (Vₚ, upgradeable)
  │
  ▼
4. Greedy MCKP          Algorithm 1: pin core at raw, start peripherals
   allocation           at stub, prune lowest-S(v) if over budget, then
   O(|V| log |V|)       upgrade by marginal efficiency ρ = ΔU/Δc
  │
  ▼
5. SACRS                render each selected symbol at its chosen level
   materialization      (raw / sig+docs / interface / stub)
  │
  ▼
Assembled context + per-symbol citations [NodeType, k=level]
```

---

## SACRS representation levels

SACRS (Semantic-Aware Adaptive Context Representation Selection) defines four compression levels `k ∈ {0,1,2,3}`:

| Level | k | What it keeps | When the allocator chooses it |
|---|---|---|---|
| **Raw** | 0 | Signature + body + doc comment | Core symbols; high-relevance symbols when budget allows |
| **Sig + Docs** | 1 | Signature + return type + API docs (body dropped) | Need the contract, not the implementation |
| **Interface** | 2 | Signature only | Need the call shape |
| **Stub** | 3 | Symbol name only | Just need to know the symbol exists |

The four design principles from the CORE doc:

1. **Preserve type information** — DTOs keep field types (k=1 retains them).
2. **Preserve call interfaces** — Services keep signatures.
3. **Preserve framework semantics** — Controllers keep annotations/decorators (carried in the doc text).
4. **Strip executable implementation details** — Utilities compress aggressively; their bodies carry little reasoning value.

Implementation: [`internal/corerag/representations.go`](../internal/corerag/representations.go).

---

## Compression priors R(k, Type)

Each `(NodeType, Level)` pair carries an empirical retention value `R(k, Type) ∈ [0,1]` — the average fraction of reasoning-relevant information preserved when a node of that type is represented at level `k` instead of raw (k=0). These come from the CORE doc's Table 1:

| Type | k=0 | k=1 | k=2 | k=3 |
|---|---|---|---|---|
| Controller | 1.00 | 0.95 | 0.80 | 0.40 |
| DTO | 1.00 | 0.92 | 0.75 | 0.35 |
| Service | 1.00 | 0.88 | 0.65 | 0.30 |
| Utility | 1.00 | 0.70 | 0.45 | 0.20 |
| Other | 1.00 | 0.75 | 0.50 | 0.25 |

The default priors are in [`config.go`](../internal/corerag/config.go) (`DefaultPriors`) and can be overridden per type and level via the `corerag.priors` config key. The `PriorTable` accessor falls back to the `Other` row for unknown types and never over-compresses an unknown configuration.

---

## Relevance score S(v)

Each candidate symbol gets a relevance score combining four factors:

```
S(v) = α · Semantic + β · EdgeWeight + γ · Centrality − δ · UtilityPenalty
```

clamped to `[0, 1]`. Default weights: `α=0.4, β=0.2, γ=0.2, δ=0.2`.

| Factor | Meaning |
|---|---|
| `Semantic` | Cosine similarity between the query embedding and the symbol's retrieval embedding (clamped to `[0,1]` so negative similarity → 0) |
| `EdgeWeight` | Average weight of incoming edges |
| `Centrality` | `out_degree / max_out_degree` (a proxy for how much the symbol orchestrates) |
| `UtilityPenalty` | `ln(1 + in_degree)` (highly-called helpers compress more readily) |

A node is only expanded during BFS if `S(v) ≥ θ` (default `θ = 0.15`). Seeds are always included regardless of score.

Implementation: [`internal/corerag/relevance.go`](../internal/corerag/relevance.go).

---

## The greedy allocator (Algorithm 1)

The CORE doc specifies a Budget-Aware Marginal Utility Greedy Allocation. The implementation in [`selector.go`](../internal/corerag/selector.go) runs four phases:

1. **Pin core nodes** `V₀` at `LevelRaw` (full source). Their cost is deducted from the budget first; core nodes are never compressed or pruned.

2. **Start peripherals** `Vₚ` at `LevelStub` (cheapest). If even the stubs exceed the budget, the lowest-`S(v)` peripherals are pruned until it fits.

3. **Build a transition pool** of upgrades `k → k−1`, each with:
   - marginal utility `ΔU = S(v)·R(k−1, Type) − S(v)·R(k, Type)`
   - marginal cost `Δc = cost(k−1) − cost(k)`
   - efficiency `ρ = ΔU / Δc`
   Free upgrades (`Δc ≤ 0`) are applied immediately; paid upgrades go into the pool.

4. **Apply upgrades** in decreasing `ρ` order while the budget allows. After each upgrade, the node's next upgrade is offered back into the pool, looping past consecutive free upgrades so a node is never left stranded between levels.

Complexity is `O(|V| log |V|)`, dominated by sorting the transition pool (CORE doc §4.3).

### Edge cases handled

- **Pruning direction** — peripherals are sorted descending by `S(v)` so the lowest-relevance node is at the end and pruned first (matches Algorithm 1).
- **Free upgrades** — both the re-validation branch and the post-upgrade branch loop through consecutive `Δc ≤ 0` upgrades rather than stopping after one.
- **Budget overshoot** — core nodes are pinned at raw unconditionally; if their raw cost alone exceeds the budget, usage may overshoot (Phase 2 pruning only applies to peripherals). This is documented behavior.

---

## Node classification

Symbols are classified into `NodeType` by language-specific heuristics in [`classifier.go`](../internal/corerag/classifier.go):

- **Controller** — framework entry points (HTTP handlers, route callbacks); annotations/decorators carry architectural semantics.
- **DTO** — data definitions (models, schemas, entities) whose field types are reasoning-essential.
- **Service** — business-logic orchestrators that call other symbols.
- **Utility** — peripheral helpers (logging, validation, formatting) whose bodies carry little reasoning value.
- **Other** — fallback for symbols that match no known role.

Classification uses naming conventions (`*Service`, `*Controller`, `Handler.ServeHTTP`), structural cues (struct with `json` tags → DTO), and language markers (decorators in Python/Java, annotations in Java).

---

## Parsers

| Language | Parser | Status |
|---|---|---|
| **Go** | `go/parser` stdlib | Full (functions, methods, types, call + import edges) |
| Python | tree-sitter | Stub (cgo build tag) |
| TypeScript/JavaScript | tree-sitter | Stub (cgo build tag) |
| Java | tree-sitter | Stub (cgo build tag) |

The Go parser is pure stdlib and always available. Tree-sitter parsers require cgo (`CGO_ENABLED=1`); when cgo is disabled, [`ts_parsers_stub.go`](../internal/corerag/ts_parsers_stub.go) returns `nil` so the engine degrades gracefully to Go-only indexing. Call edges are static-approximate (callee names, resolved to known symbol IDs by name within the repo) — this matches the documented limitation for all graph-based code retrieval (CORE doc §7.4).

---

## Embedding backends

The retrieval embedding is computed from each symbol's `Signature + Doc + Name`. Four backends live in [`internal/embeddings`](../internal/embeddings):

| Provider | Constant | When | Notes |
|---|---|---|---|
| ONNX | `onnx` | Local, offline, most accurate | Requires `libonnxruntime` + a BGE/E5 model; mean-pools + L2-normalizes; build-tagged stub keeps the binary compiling with `CGO_ENABLED=0` |
| API | `api` | No local model, have an embeddings endpoint | Uses the OpenAI-compatible `/embeddings` route via `llm.OpenAIClient.CreateEmbeddings` |
| Ollama | `ollama` | Local via `ollama serve` | Calls `localhost:11434/api/embeddings` |
| Mock | `mock` | Tests / offline dev | Deterministic FNV-1a hash vectors, L2-normalized, no dependencies |

The factory ([`embedder.go`](../internal/embeddings/embedder.go)) tries the configured `provider` and falls back to `fallback` if init fails, so the engine keeps working when a model isn't installed. A `HeuristicTokenCounter` (`≈ fields × 1.33`) estimates token cost; a real tokenizer lands in a later spec for tighter budgets.

---

## Persistence

Symbols, edges, and repo metadata are stored in SQLite via [`store.go`](../internal/corerag/store.go). The store **shares the same `*sql.DB` connection** as the memory store (opened in `cmd/agent/main.go`, passed via `store.DB()`) rather than opening its own — matching the `background.NewStore` convention and keeping a single connection per process.

Schema:

- `corerag_repos` — repo id, path, content hash, language, timestamps
- `corerag_symbols` — id, repo, path, name, kind, node_type, signature, doc, body, byte offsets, embedding (JSON), in/out-degree
- `corerag_edges` — repo, src, dst, kind, weight

Re-indexing an unchanged repo (same content hash) is a no-op that reports the persisted counts.

---

## Configuration

See the `corerag` block in [`config.example.yaml`](../config.example.yaml). Key knobs:

| Key | Default | Meaning |
|---|---|---|
| `enabled` | `false` | Master switch; wiring in `main.go` is guarded by this |
| `budget` | `4096` | Token budget `B` for the assembled context |
| `depth` | `2` | BFS expansion depth `k` |
| `embedding.provider` | `onnx` | Primary embedding backend |
| `embedding.fallback` | `api` | Fallback backend if primary fails to init |
| `embedding.model` | `bge-small-en-v1.5` | Model name / hint |
| `embedding.dim` | `384` | Embedding dimensionality |
| `relevance.alpha` | `0.4` | Semantic weight |
| `relevance.beta` | `0.2` | Edge-weight weight |
| `relevance.gamma` | `0.2` | Centrality weight |
| `relevance.delta` | `0.2` | Utility-penalty weight |
| `relevance.theta` | `0.15` | Minimum `S(v)` to expand a node |
| `priors` | (defaults) | Optional `R(k, Type)` overrides |

`Validate()` checks the weights are non-negative, `θ ∈ [0,1]`, and `budget > 0`, `depth ≥ 0`.

---

## Tools

The engine exposes two tools, registered in [`cmd/agent/main.go`](../cmd/agent/main.go) when `corerag.enabled` is true (soft-failure on init error, matching the MCP/browser pattern):

### `index_repo`

Walks a repository, parses symbols and call/import edges, classifies node types, computes graph degrees, embeds symbols, and persists to SQLite. Skips unchanged repos by content hash unless `force: true`.

```json
{"action": "index", "path": "/abs/path/to/repo", "language": "go", "force": false}
```

Returns `{repo_id, path, language, files, symbols, edges, hash, duration_ms}`.

### `repo_query`

Given a natural-language query, runs the full pipeline and returns assembled context with per-symbol citations.

```json
{"action": "query", "query": "how does auth work?", "repo": "/abs/path/to/repo", "budget": 4096, "top_k": 8}
```

Returns `{repo_id, context, tokens_used, nodes, core_count, peripheral_count, seeds}`. The `context` field is a Markdown block tagging each symbol with its `[NodeType, k=level]`.

---

## Roadmap (Specs 2–4)

Spec 1 (this) delivers the shared core + Code-RAG MVP. Remaining work per the approved plan:

| Spec | Scope | Status |
|---|---|---|
| **1** | Shared core (embeddings + corerag) + Code-RAG MVP (Go parser, ONNX/API/Ollama/Mock, greedy allocator, two tools) | ✅ Shipped |
| **2** | Cross-encoder rerank (ONNX) for tighter semantic ranking | Pending |
| **3** | Full `R(k, Type)` estimation harness to replace the Table 1 defaults with measured priors | Pending |
| **4** | Memory-recall upgrade: reuse the shared embeddings core for conversation memory (fixes a nil-embedding bug at `main.go:364`) | Pending |

---

## Limitations

- **Multi-language parsers** — only Go is fully parsed; Python/TS/JS/Java tree-sitter parsers are stubs pending cgo wiring.
- **Call-edge resolution** — static-approximate (callee names, no cross-package type resolution).
- **Token counting** — heuristic; a real tokenizer arrives in a later spec.
- **Priors** — Table 1 defaults are empirical; Spec 3 will measure them per-repo.
