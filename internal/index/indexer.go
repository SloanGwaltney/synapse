package index

import (
	"fmt"
	"os"
	"path/filepath"

	"synapse/internal/chunker"
	"synapse/internal/chunker/languages"
	"synapse/internal/embedder"
	"synapse/internal/llm"
	"synapse/internal/store"
)

// Config holds the indexer configuration.
type Config struct {
	DBPath        string
	OllamaURL     string
	Model         string
	Workers       int
	OverviewModel string
}

// Indexer is the public API for indexing and searching codebases.
type Indexer struct {
	store    *store.SQLiteStore
	embedder *embedder.OllamaEmbedder
	chunker  *chunker.ASTChunker
	registry *chunker.Registry
	config   Config
}

// New creates a new Indexer with the given configuration.
func New(cfg Config) (*Indexer, error) {
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}

	reg := chunker.NewRegistry()
	languages.RegisterGo(reg)
	languages.RegisterJavaScript(reg)
	languages.RegisterTypeScript(reg)
	languages.RegisterPython(reg)

	return &Indexer{
		store:    s,
		embedder: embedder.NewOllamaEmbedder(cfg.OllamaURL, cfg.Model),
		chunker:  chunker.NewASTChunker(reg),
		registry: reg,
		config:   cfg,
	}, nil
}

// Index indexes the codebase at the given root path.
func (idx *Indexer) Index(root string) (*Stats, error) {
	// Check if the embedding model changed since last indexing.
	lastModel, err := idx.store.GetMeta("embedding_model")
	if err != nil {
		return nil, fmt.Errorf("get meta: %w", err)
	}
	if lastModel != "" && lastModel != idx.config.Model {
		fmt.Printf("Embedding model changed from %q to %q — re-indexing all files\n", lastModel, idx.config.Model)
		if err := idx.store.DeleteAllChunks(); err != nil {
			return nil, fmt.Errorf("delete all chunks: %w", err)
		}
	}

	stats, err := runPipeline(root, idx.store, idx.chunker, idx.registry, idx.embedder, idx.config.Workers)
	if err != nil {
		return nil, err
	}

	if err := idx.store.SetMeta("embedding_model", idx.config.Model); err != nil {
		return nil, fmt.Errorf("set meta: %w", err)
	}

	// Generate project overview if files were indexed.
	if stats.FilesIndexed > 0 {
		overviewModel := idx.config.OverviewModel
		if overviewModel == "" {
			overviewModel = "qwen3:8b"
		}
		chat := llm.NewOllamaChat(idx.config.OllamaURL, overviewModel)

		fmt.Println("Generating file summaries...")
		if err := summarizeFiles(idx.store, chat); err != nil {
			fmt.Fprintf(os.Stderr, "warning: file summarization failed: %v\n", err)
		}

		fmt.Println("Generating project overview...")
		overview, err := synthesizeOverview(idx.store, chat)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: overview generation failed: %v\n", err)
		} else {
			overviewPath := filepath.Join(filepath.Dir(idx.config.DBPath), "overview.md")
			if err := os.WriteFile(overviewPath, []byte(overview), 0o644); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to write overview: %v\n", err)
			}
		}
	}

	return stats, nil
}

// Search finds the top-k chunks closest to the query.
func (idx *Indexer) Search(query string, k int) ([]store.SearchResult, error) {
	embedding, err := idx.embedder.EmbedSingle(query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	return idx.store.Search(embedding, k)
}

// Close releases resources.
func (idx *Indexer) Close() error {
	return idx.store.Close()
}
