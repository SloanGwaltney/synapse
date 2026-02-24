package index

import (
	"fmt"
	"strings"

	"synapse/internal/llm"
	"synapse/internal/store"
)

const fileSummaryPrompt = `Summarize this source file in 2-3 sentences. What does it define, and what is its role in the project? Be specific about the types, functions, or interfaces it provides. Do not speculate about things not shown in the code.

File: %s
Language: %s

` + "```\n%s\n```"

const overviewPrompt = `You are a senior software architect analyzing a codebase. Based ONLY on the file summaries and symbol names provided below, write a concise architectural overview in Markdown.

Rules:
- ONLY describe what you can directly observe in the provided summaries
- Do NOT guess or infer features that aren't shown
- Do NOT describe external tools or services — describe THIS project
- Use the file summaries and symbol names to understand purpose

Cover:
1. What the project does (one paragraph, based on the summaries you see)
2. Major components/packages and how they connect (bullet points)
3. Key data flows through the system

Keep it under 300 words. Do not include code snippets.
`

// summarizeFiles generates per-file summaries for any files that don't have one yet.
func summarizeFiles(s *store.SQLiteStore, chat *llm.OllamaChat) error {
	files, err := s.ListFiles()
	if err != nil {
		return fmt.Errorf("list files: %w", err)
	}

	for _, f := range files {
		if f.Summary != "" {
			continue
		}

		fmt.Printf("  Summarizing %s...\n", f.Path)

		content, err := s.GetAllFileContent(f.Path)
		if err != nil {
			return fmt.Errorf("get content for %s: %w", f.Path, err)
		}
		if content == "" {
			continue
		}

		prompt := fmt.Sprintf(fileSummaryPrompt, f.Path, f.Language, content)
		msgs := []llm.Message{
			{Role: "user", Content: prompt},
		}

		summary, err := chat.Generate(msgs)
		if err != nil {
			return fmt.Errorf("summarize %s: %w", f.Path, err)
		}

		if err := s.SetFileSummary(f.Path, strings.TrimSpace(summary)); err != nil {
			return fmt.Errorf("save summary for %s: %w", f.Path, err)
		}
	}

	return nil
}

// synthesizeOverview combines all file summaries into a project-level architectural overview.
func synthesizeOverview(s *store.SQLiteStore, chat *llm.OllamaChat) (string, error) {
	files, err := s.ListFiles()
	if err != nil {
		return "", fmt.Errorf("list files: %w", err)
	}
	if len(files) == 0 {
		return "", fmt.Errorf("no files indexed")
	}

	chunks, err := s.ListTopChunks()
	if err != nil {
		return "", fmt.Errorf("list chunks: %w", err)
	}

	// Group named chunks by file path.
	chunksByFile := make(map[string][]store.ChunkSummary)
	for _, c := range chunks {
		chunksByFile[c.FilePath] = append(chunksByFile[c.FilePath], c)
	}

	var b strings.Builder
	b.WriteString(overviewPrompt)
	b.WriteString("\n## Project Structure\n\n")

	for _, f := range files {
		fmt.Fprintf(&b, "### %s  (%s, %d chunks)\n", f.Path, f.Language, f.Chunks)

		if f.Summary != "" {
			fmt.Fprintf(&b, "Summary: %s\n", f.Summary)
		}

		if fileChunks, ok := chunksByFile[f.Path]; ok {
			for _, c := range fileChunks {
				fmt.Fprintf(&b, "  - [%s] %s\n", c.Kind, c.Name)
			}
		}
		b.WriteString("\n")
	}

	msgs := []llm.Message{
		{Role: "user", Content: b.String()},
	}

	return chat.Generate(msgs)
}
