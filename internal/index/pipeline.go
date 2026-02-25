package index

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"

	"synapse/internal/chunker"
	"synapse/internal/embedder"
	"synapse/internal/store"
	"synapse/internal/walker"
)

const embedBatchSize = 32

// Stats reports indexing results.
type Stats struct {
	FilesTotal   int
	FilesIndexed int
	FilesSkipped int
	ChunksTotal  int
}

// fileWork is a file that needs to be (re-)indexed.
type fileWork struct {
	info walker.FileInfo
	hash string
	lang string
	src  []byte
}

// chunkBatch is the chunks extracted from a single file.
type chunkBatch struct {
	work   fileWork
	chunks []chunker.RawChunk
}

// embeddedBatch has chunks with their embeddings ready to store.
type embeddedBatch struct {
	work       fileWork
	chunks     []chunker.RawChunk
	embeddings [][]float32
}

func runPipeline(
	root string,
	s *store.SQLiteStore,
	astChunker *chunker.ASTChunker,
	registry *chunker.Registry,
	emb *embedder.OllamaEmbedder,
	numWorkers int,
	onProgress ProgressFunc,
) (*Stats, error) {
	if numWorkers <= 0 {
		numWorkers = runtime.NumCPU()
	}

	var stats Stats
	var filesTotal atomic.Int64

	// Stage 1: Walk (only files with registered grammars)
	fileCh, walkErrCh := walker.Walk(root, registry.Extensions())

	// Stage 2: Hash + check (N workers)
	workCh := make(chan fileWork, numWorkers)
	var hashWg sync.WaitGroup
	for range numWorkers {
		hashWg.Add(1)
		go func() {
			defer hashWg.Done()
			for fi := range fileCh {
				filesTotal.Add(1)
				src, err := os.ReadFile(fi.Path)
				if err != nil {
					continue
				}
				h := sha256.Sum256(src)
				hash := hex.EncodeToString(h[:])

				existing, err := s.GetFileHash(fi.RelPath)
				if err == nil && existing == hash {
					continue // unchanged
				}

				lang := registry.LanguageName(fi.Path)
				workCh <- fileWork{
					info: fi,
					hash: hash,
					lang: lang,
					src:  src,
				}
			}
		}()
	}
	go func() {
		hashWg.Wait()
		close(workCh)
	}()

	// Stage 3: Chunk (N workers)
	chunkCh := make(chan chunkBatch, numWorkers)
	var chunkWg sync.WaitGroup
	for range numWorkers {
		chunkWg.Add(1)
		go func() {
			defer chunkWg.Done()
			for w := range workCh {
				chunks, err := astChunker.Chunk(w.info.RelPath, w.src)
				if err != nil {
					fmt.Fprintf(os.Stderr, "chunker error %s: %v\n", w.info.RelPath, err)
					continue
				}
				if len(chunks) > 0 {
					chunkCh <- chunkBatch{work: w, chunks: chunks}
				}
			}
		}()
	}
	go func() {
		chunkWg.Wait()
		close(chunkCh)
	}()

	// Stage 4: Embed (1 worker, batches of embedBatchSize)
	embeddedCh := make(chan embeddedBatch, 4)
	var embedErr error
	var embedWg sync.WaitGroup
	embedWg.Add(1)
	go func() {
		defer embedWg.Done()
		defer close(embeddedCh)

		for batch := range chunkCh {
			texts := make([]string, len(batch.chunks))
			for i, c := range batch.chunks {
				texts[i] = c.Content
			}

			// Embed in sub-batches of embedBatchSize.
			allEmbeddings := make([][]float32, 0, len(texts))
			for i := 0; i < len(texts); i += embedBatchSize {
				end := i + embedBatchSize
				if end > len(texts) {
					end = len(texts)
				}
				embs, err := emb.Embed(texts[i:end])
				if err != nil {
					fmt.Fprintf(os.Stderr, "embed error %s: %v\n", batch.work.info.RelPath, err)
					embedErr = err
					return
				}
				allEmbeddings = append(allEmbeddings, embs...)
			}

			embeddedCh <- embeddedBatch{
				work:       batch.work,
				chunks:     batch.chunks,
				embeddings: allEmbeddings,
			}
		}
	}()

	// Stage 5: Store (1 worker)
	var storeErr error
	var storeWg sync.WaitGroup
	storeWg.Add(1)
	go func() {
		defer storeWg.Done()

		for eb := range embeddedCh {
			fileID, err := s.UpsertFile(store.FileRecord{
				Path:      eb.work.info.RelPath,
				Hash:      eb.work.hash,
				Language:  eb.work.lang,
				SizeBytes: eb.work.info.Size,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "store upsert error %s: %v\n", eb.work.info.RelPath, err)
				storeErr = err
				continue
			}

			storeChunks := make([]store.Chunk, len(eb.chunks))
			for i, c := range eb.chunks {
				storeChunks[i] = store.Chunk{
					Name:      c.Name,
					Kind:      c.Kind,
					StartLine: c.StartLine,
					EndLine:   c.EndLine,
					Content:   c.Content,
				}
			}

			chunkIDs, err := s.InsertChunks(fileID, storeChunks)
			if err != nil {
				fmt.Fprintf(os.Stderr, "store chunks error %s: %v\n", eb.work.info.RelPath, err)
				storeErr = err
				continue
			}

			if err := s.InsertEmbeddings(chunkIDs, eb.embeddings); err != nil {
				fmt.Fprintf(os.Stderr, "store embeddings error %s: %v\n", eb.work.info.RelPath, err)
				storeErr = err
				continue
			}

			stats.FilesIndexed++
			stats.ChunksTotal += len(eb.chunks)
			if onProgress != nil {
				onProgress("Indexing files...", stats.FilesIndexed, int(filesTotal.Load()))
			}
		}
	}()

	// Wait for all stages to complete.
	storeWg.Wait()
	embedWg.Wait()

	// Check walk errors.
	if err := <-walkErrCh; err != nil {
		return nil, fmt.Errorf("walk error: %w", err)
	}

	stats.FilesTotal = int(filesTotal.Load())
	stats.FilesSkipped = stats.FilesTotal - stats.FilesIndexed

	if embedErr != nil {
		return &stats, fmt.Errorf("embedding failed: %w", embedErr)
	}
	if storeErr != nil {
		return &stats, fmt.Errorf("storage failed: %w", storeErr)
	}

	return &stats, nil
}
