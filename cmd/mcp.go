package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"synapse/internal/embedder"
	"synapse/internal/rag"
	"synapse/internal/store"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start an MCP server exposing codebase search tools",
	RunE:  runMCP,
}

func runMCP(cmd *cobra.Command, args []string) error {
	// Resolve DB path — same pattern as chat.go.
	dbPath := flagDB
	if dbPath == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		dbPath = filepath.Join(wd, ".synapse", "index.db")
	}
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return fmt.Errorf("index not found at %s\nRun 'synapse index <path>' first to build the index", dbPath)
	}

	st, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open index: %w", err)
	}
	defer st.Close()

	emb := embedder.NewOllamaEmbedder(flagOllama, flagModel)
	overviewPath := filepath.Join(filepath.Dir(dbPath), "overview.md")

	s := mcpserver.NewMCPServer("synapse", "1.0.0", mcpserver.WithToolCapabilities(false))

	s.AddTool(searchCodebaseTool(), makeSearchHandler(st, emb))
	s.AddTool(getFileSummaryTool(), makeFileSummaryHandler(st))
	s.AddTool(getProjectOverviewTool(), makeOverviewHandler(overviewPath))
	s.AddTool(listIndexedFilesTool(), makeListFilesHandler(st))

	return mcpserver.ServeStdio(s)
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}

// --- Tool schema builders ---

var readOnlyAnnotation = mcp.ToolAnnotation{
	ReadOnlyHint:    mcp.ToBoolPtr(true),
	DestructiveHint: mcp.ToBoolPtr(false),
	IdempotentHint:  mcp.ToBoolPtr(true),
	OpenWorldHint:   mcp.ToBoolPtr(false),
}

func searchCodebaseTool() mcp.Tool {
	return mcp.NewTool("search_codebase",
		mcp.WithDescription("Semantically search the indexed codebase using hybrid BM25 + vector similarity. Returns relevant code chunks with file paths and line numbers."),
		mcp.WithToolAnnotation(readOnlyAnnotation),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Natural language or keyword query to search the codebase"),
		),
		mcp.WithNumber("k",
			mcp.Description("Maximum number of chunks to return (default 10)"),
		),
	)
}

func getFileSummaryTool() mcp.Tool {
	return mcp.NewTool("get_file_summary",
		mcp.WithDescription("Get the LLM-generated summary and metadata for a specific indexed file."),
		mcp.WithToolAnnotation(readOnlyAnnotation),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("File path as indexed (relative to the project root)"),
		),
	)
}

func getProjectOverviewTool() mcp.Tool {
	return mcp.NewTool("get_project_overview",
		mcp.WithDescription("Get the high-level project overview synthesized from all file summaries during indexing."),
		mcp.WithToolAnnotation(readOnlyAnnotation),
	)
}

func listIndexedFilesTool() mcp.Tool {
	return mcp.NewTool("list_indexed_files",
		mcp.WithDescription("List all files in the index with their language, chunk count, and summary snippet."),
		mcp.WithToolAnnotation(readOnlyAnnotation),
		mcp.WithString("language",
			mcp.Description("Optional language filter (e.g. 'go', 'python'). Case-insensitive."),
		),
	)
}

// --- Handler factories ---

func makeSearchHandler(st store.Store, emb *embedder.OllamaEmbedder) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query := req.GetString("query", "")
		if query == "" {
			return mcp.NewToolResultError("query is required"), nil
		}
		k := req.GetInt("k", 10)
		if k <= 0 {
			k = 10
		}

		chunks, err := rag.HybridRetrieve(query, st, emb, k)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("search failed: %v", err)), nil
		}

		return mcp.NewToolResultText(formatSearchResults(query, chunks)), nil
	}
}

func makeFileSummaryHandler(st store.Store) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path := req.GetString("path", "")
		if path == "" {
			return mcp.NewToolResultError("path is required"), nil
		}

		files, err := st.ListFiles()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("list files failed: %v", err)), nil
		}

		for _, f := range files {
			if f.Path == path {
				summary := f.Summary
				if summary == "" {
					summary = "(No summary generated yet)"
				}
				return mcp.NewToolResultText(fmt.Sprintf("## %s\n\n**Language:** %s  \n**Chunks:** %d\n\n%s",
					f.Path, f.Language, f.Chunks, summary)), nil
			}
		}

		return mcp.NewToolResultError(fmt.Sprintf("file %q not found in index — call list_indexed_files to see available paths", path)), nil
	}
}

func makeOverviewHandler(overviewPath string) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		data, err := os.ReadFile(overviewPath)
		if err != nil {
			if os.IsNotExist(err) {
				return mcp.NewToolResultText("No overview available yet. Run 'synapse index <path>' to generate one."), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("read overview failed: %v", err)), nil
		}
		if len(data) == 0 {
			return mcp.NewToolResultText("Overview file exists but is empty."), nil
		}
		return mcp.NewToolResultText(string(data)), nil
	}
}

func makeListFilesHandler(st store.Store) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		langFilter := strings.ToLower(req.GetString("language", ""))

		files, err := st.ListFiles()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("list files failed: %v", err)), nil
		}

		var filtered []store.FileSummary
		for _, f := range files {
			if langFilter == "" || strings.ToLower(f.Language) == langFilter {
				filtered = append(filtered, f)
			}
		}

		var sb strings.Builder
		if langFilter != "" {
			fmt.Fprintf(&sb, "## Indexed files (%d, language: %s)\n\n", len(filtered), langFilter)
		} else {
			fmt.Fprintf(&sb, "## Indexed files (%d)\n\n", len(filtered))
		}

		for _, f := range filtered {
			snippet := f.Summary
			if idx := strings.Index(snippet, "\n"); idx >= 0 {
				snippet = snippet[:idx]
			}
			if len(snippet) > 120 {
				snippet = snippet[:120] + "..."
			}
			if snippet == "" {
				snippet = "(no summary)"
			}
			fmt.Fprintf(&sb, "- **%s** (%s, %d chunks) — %s\n", f.Path, f.Language, f.Chunks, snippet)
		}

		return mcp.NewToolResultText(sb.String()), nil
	}
}

// --- Formatting helpers ---

func formatSearchResults(query string, chunks []store.SearchResult) string {
	if len(chunks) == 0 {
		return fmt.Sprintf("No results found for query: %q", query)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Search results for %q (%d chunks)\n\n", query, len(chunks))

	for i, c := range chunks {
		fmt.Fprintf(&sb, "### Result %d: `%s`\n\n", i+1, c.FilePath)
		fmt.Fprintf(&sb, "**Kind:** %s  \n**Name:** %s  \n**Lines:** %d–%d  \n**Language:** %s\n\n",
			c.Chunk.Kind, c.Chunk.Name, c.Chunk.StartLine, c.Chunk.EndLine, c.Language)
		fmt.Fprintf(&sb, "```%s\n%s\n```\n\n", strings.ToLower(c.Language), c.Chunk.Content)
	}

	return sb.String()
}
