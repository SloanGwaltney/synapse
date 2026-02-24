package chunker

import (
	"context"
	"fmt"
	"sort"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

const maxChunkBytes = 8192

// RawChunk is a chunk extracted from a source file before embedding.
type RawChunk struct {
	Name      string
	Kind      string
	StartLine int
	EndLine   int
	Content   string
}

// ASTChunker parses source files using tree-sitter and extracts semantic chunks.
type ASTChunker struct {
	registry *Registry
}

// NewASTChunker creates a chunker backed by the given registry.
func NewASTChunker(r *Registry) *ASTChunker {
	return &ASTChunker{registry: r}
}

// Chunk parses the source and returns semantic chunks. If no grammar is
// registered for the file, it returns nil (caller should use fallback).
func (c *ASTChunker) Chunk(path string, src []byte) ([]RawChunk, error) {
	spec, lang := c.registry.Lookup(path)
	if spec == nil {
		return nil, nil
	}

	parser := sitter.NewParser()
	parser.SetLanguage(spec.Language)
	tree, err := parser.ParseCtx(context.Background(), nil, src)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	defer tree.Close()

	q, err := sitter.NewQuery([]byte(spec.Query), spec.Language)
	if err != nil {
		return nil, fmt.Errorf("compile query for %s: %w", lang, err)
	}
	defer q.Close()

	qc := sitter.NewQueryCursor()
	defer qc.Close()
	qc.Exec(q, tree.RootNode())

	var captures []capture
	for {
		m, ok := qc.NextMatch()
		if !ok {
			break
		}
		var chunkNode *sitter.Node
		var nameStr string
		for _, cap := range m.Captures {
			capName := q.CaptureNameForId(cap.Index)
			switch capName {
			case "chunk":
				chunkNode = cap.Node
			case "name":
				nameStr = cap.Node.Content(src)
			}
		}
		if chunkNode == nil {
			continue
		}
		captures = append(captures, capture{
			name:      nameStr,
			kind:      chunkNode.Type(),
			startLine: int(chunkNode.StartPoint().Row) + 1,
			endLine:   int(chunkNode.EndPoint().Row) + 1,
			startByte: chunkNode.StartByte(),
			endByte:   chunkNode.EndByte(),
		})
	}

	// Deduplicate: when captures overlap, keep only the outer (larger) node.
	captures = dedup(captures)

	// Build chunks with context enrichment.
	lines := strings.Split(string(src), "\n")
	var chunks []RawChunk
	for _, cap := range captures {
		content := enrichContent(path, lang, cap.kind, cap.name, lines, cap.startLine, cap.endLine)

		if len(content) > maxChunkBytes {
			splits := splitOversized(content, cap.name, cap.kind, cap.startLine)
			chunks = append(chunks, splits...)
		} else {
			chunks = append(chunks, RawChunk{
				Name:      cap.name,
				Kind:      cap.kind,
				StartLine: cap.startLine,
				EndLine:   cap.endLine,
				Content:   content,
			})
		}
	}

	return chunks, nil
}

// dedup removes captures that are fully contained within a larger capture.
func dedup(caps []capture) []capture {
	if len(caps) <= 1 {
		return caps
	}
	// Sort by start byte ascending, then by size descending (larger first).
	sort.Slice(caps, func(i, j int) bool {
		if caps[i].startByte != caps[j].startByte {
			return caps[i].startByte < caps[j].startByte
		}
		return (caps[i].endByte - caps[i].startByte) > (caps[j].endByte - caps[j].startByte)
	})

	var result []capture
	var lastEnd uint32
	for _, c := range caps {
		if c.startByte >= lastEnd || lastEnd == 0 {
			result = append(result, c)
			if c.endByte > lastEnd {
				lastEnd = c.endByte
			}
		}
		// Skip captures contained within the previous one.
	}
	return result
}

func enrichContent(path, lang, kind, name string, lines []string, startLine, endLine int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "// File: %s\n", path)
	fmt.Fprintf(&b, "// Language: %s\n", lang)
	if name != "" {
		fmt.Fprintf(&b, "// %s: %s\n", kind, name)
	}
	// Lines are 1-indexed.
	start := startLine - 1
	end := endLine
	if start < 0 {
		start = 0
	}
	if end > len(lines) {
		end = len(lines)
	}
	for i := start; i < end; i++ {
		b.WriteString(lines[i])
		if i < end-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// splitOversized splits a chunk that exceeds maxChunkBytes into smaller pieces
// at line boundaries with 10-line overlap.
func splitOversized(content, name, kind string, baseStartLine int) []RawChunk {
	lines := strings.Split(content, "\n")
	const windowSize = 40
	const overlap = 10

	var chunks []RawChunk
	for i := 0; i < len(lines); {
		end := i + windowSize
		if end > len(lines) {
			end = len(lines)
		}
		chunk := strings.Join(lines[i:end], "\n")
		chunks = append(chunks, RawChunk{
			Name:      name,
			Kind:      kind,
			StartLine: baseStartLine + i,
			EndLine:   baseStartLine + end - 1,
			Content:   chunk,
		})
		if end >= len(lines) {
			break
		}
		i += windowSize - overlap
	}
	return chunks
}

type capture struct {
	name      string
	kind      string
	startLine int
	endLine   int
	startByte uint32
	endByte   uint32
}
