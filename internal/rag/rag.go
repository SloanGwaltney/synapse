package rag

import (
	"fmt"
	"strings"

	"synapse/internal/embedder"
	"synapse/internal/llm"
	"synapse/internal/store"
)

const systemPrompt = `You are a code intelligence assistant. You answer questions about a codebase using the retrieved source code context provided below.

Focus on answering how, why, and where questions about the code. Explain architecture, data flow, and relationships between components. Reference specific file paths and line numbers when relevant.

Do not generate new code unless explicitly asked. Keep answers concise and grounded in the provided context. If the context doesn't contain enough information to answer, say so.`

// HybridRetrieve runs both FTS5 keyword search and vector similarity search,
// then merges and deduplicates results with BM25 matches first.
func HybridRetrieve(query string, st store.Store, emb *embedder.OllamaEmbedder, k int) ([]store.SearchResult, error) {
	// Run both searches.
	ftsResults, ftsErr := st.FTSSearch(query, k)
	// FTS errors (e.g. syntax issues in query) are non-fatal — fall back to vector only.
	if ftsErr != nil {
		ftsResults = nil
	}

	vec, err := emb.EmbedSingle(query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	vecResults, err := st.Search(vec, k)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}

	// Merge: BM25 results first, then vector results, deduplicated by chunk ID.
	seen := make(map[int64]bool)
	var merged []store.SearchResult

	for _, r := range ftsResults {
		if !seen[r.Chunk.ID] {
			seen[r.Chunk.ID] = true
			merged = append(merged, r)
		}
	}
	for _, r := range vecResults {
		if !seen[r.Chunk.ID] {
			seen[r.Chunk.ID] = true
			merged = append(merged, r)
		}
	}

	if len(merged) > k {
		merged = merged[:k]
	}
	return merged, nil
}

// BuildMessages constructs the message list for the LLM from retrieved chunks,
// conversation history, and the current question.
func BuildMessages(chunks []store.SearchResult, history []llm.Message, question string, overview string) []llm.Message {
	var msgs []llm.Message

	// System message with optional overview.
	sys := systemPrompt
	if overview != "" {
		sys += "\n\n## Project Overview\n\n" + overview
	}
	msgs = append(msgs, llm.Message{Role: "system", Content: sys})

	// Context message with retrieved chunks.
	if len(chunks) > 0 {
		var ctx strings.Builder
		ctx.WriteString("Here is the relevant source code context:\n\n")
		for i, c := range chunks {
			fmt.Fprintf(&ctx, "--- Chunk %d: %s [%s %s] (lines %d–%d, %s) ---\n",
				i+1, c.FilePath, c.Chunk.Kind, c.Chunk.Name,
				c.Chunk.StartLine, c.Chunk.EndLine, c.Language)
			ctx.WriteString(c.Chunk.Content)
			ctx.WriteString("\n\n")
		}
		msgs = append(msgs, llm.Message{Role: "user", Content: ctx.String()})
		msgs = append(msgs, llm.Message{Role: "assistant", Content: "I've reviewed the code context. What would you like to know?"})
	}

	// Conversation history.
	msgs = append(msgs, history...)

	// Current question.
	msgs = append(msgs, llm.Message{Role: "user", Content: question})

	return msgs
}
