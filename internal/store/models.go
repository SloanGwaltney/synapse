package store

import "time"

// FileRecord represents an indexed source file.
type FileRecord struct {
	ID        int64
	Path      string
	Hash      string
	Language  string
	IndexedAt time.Time
	SizeBytes int64
}

// Chunk represents a parsed code chunk from a source file.
type Chunk struct {
	ID        int64
	FileID    int64
	Name      string
	Kind      string
	StartLine int
	EndLine   int
	Content   string
	Metadata  string
}

// FileSummary is a lightweight file record for overview generation.
type FileSummary struct {
	Path     string
	Language string
	Chunks   int
	Summary  string
}

// ChunkSummary is a lightweight chunk record for overview generation.
type ChunkSummary struct {
	Name     string
	Kind     string
	FilePath string
}

// SearchResult is a chunk with its similarity score and file path.
type SearchResult struct {
	Chunk    Chunk
	FilePath string
	Language string
	Distance float64
}
