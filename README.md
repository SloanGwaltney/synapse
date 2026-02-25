# synapse

Local code intelligence powered by RAG. Index a codebase once, then ask questions about it in plain English or expose it as an MCP server for AI agents — entirely offline, no cloud APIs required.

```
synapse index ./my-project
synapse chat
> How does the authentication middleware work?
```

---

## How it works

1. **Index** — walks the project, parses source files into AST-aware chunks using [Tree-sitter](https://tree-sitter.github.io/tree-sitter/), embeds each chunk via Ollama, and stores everything in a local SQLite database. An LLM then generates a per-file summary and a synthesised project overview.
2. **Retrieve** — queries run hybrid search: BM25 full-text (FTS5) and vector similarity in parallel, merged and deduplicated, so keyword precision and semantic recall both work.
3. **Answer** — the top-k chunks are injected as context into an Ollama chat model, which answers grounded in the actual source code.

The index is stored in `.synapse/index.db` next to the project. Re-indexing is incremental — only files whose SHA-256 hash has changed are reprocessed.

---

## Requirements

- [Go 1.22+](https://go.dev/dl/) (with CGO enabled)
- [Ollama](https://ollama.com) running locally
- Recommended models:
  - Embedding: `nomic-embed-text` — `ollama pull nomic-embed-text`
  - Chat / summaries: `qwen3:8b` — `ollama pull qwen3:8b`

---

## Installation

```bash
git clone https://github.com/your-username/synapse
cd synapse
go build -tags sqlite_fts5 -o synapse .
```

> The `-tags sqlite_fts5` flag enables FTS5 full-text search in the SQLite driver. It must be included for every build.

Move the binary somewhere on your `$PATH`, or run it directly from the project directory.

---

## Usage

### Interactive TUI

Running `synapse` with no arguments launches the full interactive interface: it checks for an existing index, walks you through model selection if needed, runs the indexer with a live progress display, and drops into chat.

```bash
synapse
```

### CLI commands

#### `synapse index <path>`

Index a codebase (or re-index changed files).

```bash
synapse index ./my-project
synapse index . --workers 8
synapse index . --db /custom/path/index.db
```

| Flag | Default | Description |
|---|---|---|
| `--workers` | `20` | Parallel workers for hashing and chunking |
| `--overview-model` | same as `--chat-model` | Model used for per-file summaries and project overview |

#### `synapse chat`

Ask questions about the indexed codebase in a conversational loop.

```bash
synapse chat
synapse chat --k 15          # retrieve more chunks per query
synapse chat --db /path/to/index.db
```

| Flag | Default | Description |
|---|---|---|
| `--k` | `10` | Number of chunks retrieved per question |

Commands inside chat: `/clear` to reset conversation history, `/help`, `/exit`.

#### `synapse mcp`

Start a [Model Context Protocol](https://modelcontextprotocol.io) server over stdio, exposing the index as agent-callable tools.

```bash
synapse mcp
synapse mcp --db /path/to/index.db
```

See [MCP integration](#mcp-integration) below.

### Global flags

All commands inherit these flags:

| Flag | Default | Description |
|---|---|---|
| `--db` | `<cwd>/.synapse/index.db` | Path to the SQLite index |
| `--ollama` | `http://localhost:11434` | Ollama base URL |
| `--model` | `nomic-embed-text` | Embedding model |
| `--chat-model` | `qwen3:8b` | Generative model for chat and summaries |

---

## Supported languages

| Language | Extensions |
|---|---|
| Go | `.go` |
| Python | `.py` |
| JavaScript | `.js` |
| TypeScript | `.ts`, `.tsx` |

Chunks are extracted using AST queries (Tree-sitter), so the index contains named, structured units — functions, methods, types — rather than arbitrary line windows.

---

## Ignoring files

On first run, synapse creates a `.synapseignore` file in the indexed directory with sensible defaults:

```
.git
node_modules
vendor
__pycache__
dist
build
# ...
```

Edit it to add your own patterns. One pattern per line; supports exact directory names, path prefixes, and globs. Lines starting with `#` are comments.

---

## MCP integration

`synapse mcp` exposes four read-only tools that AI agents can call instead of reading source files directly. Index once, then any MCP-compatible agent gets targeted, pre-computed answers instantly — no file crawling, no repeated LLM summarisation.

| Tool | Description |
|---|---|
| `search_codebase` | Hybrid BM25 + vector search. Args: `query` (required), `k` (optional, default 10) |
| `get_file_summary` | LLM-generated summary and metadata for a specific file. Args: `path` (required) |
| `get_project_overview` | High-level project overview synthesised from all file summaries |
| `list_indexed_files` | All indexed files with language and chunk count. Args: `language` (optional filter) |

All tools are annotated `readOnly`, `idempotent`, non-destructive, and closed-world.

### Claude Code

Add to your project's `.mcp.json`:

```json
{
  "mcpServers": {
    "synapse": {
      "command": "/path/to/synapse",
      "args": ["mcp", "--db", "/path/to/project/.synapse/index.db"],
      "type": "stdio"
    }
  }
}
```

### Claude Desktop

Add to `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "synapse": {
      "command": "/path/to/synapse",
      "args": ["mcp", "--db", "/path/to/project/.synapse/index.db"]
    }
  }
}
```

---

## Project layout

```
cmd/
  root.go       # global flags, entry point
  index.go      # synapse index
  chat.go       # synapse chat
  mcp.go        # synapse mcp
  tui.go        # launches interactive TUI
internal/
  walker/       # async directory traversal, .synapseignore
  chunker/      # Tree-sitter AST chunking + language registry
  embedder/     # Ollama /api/embed client
  index/        # orchestration: pipeline, file summarisation, overview
  store/        # SQLite + sqlite-vec: schema, CRUD, FTS5, vector search
  rag/          # hybrid retrieval (BM25 + cosine), prompt assembly
  llm/          # Ollama chat client
  tui/          # Bubble Tea TUI: welcome, setup, indexing, chat screens
```

---

## Roadmap

- [ ] Github Actions Build
- [ ] Testing
- [ ] Observability / tracing
- [ ] More languages (Java, C#)
