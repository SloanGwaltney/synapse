package store

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

func init() {
	sqlite_vec.Auto()
}

// Store provides persistence for indexed files, chunks, and embeddings.
type Store interface {
	// GetFileHash returns the stored hash for a path, or "" if not indexed.
	GetFileHash(path string) (string, error)
	// UpsertFile inserts or updates a file record and returns its ID.
	// It also deletes any existing chunks and embeddings for the file.
	UpsertFile(f FileRecord) (int64, error)
	// InsertChunks inserts chunks for a file and returns their IDs.
	InsertChunks(fileID int64, chunks []Chunk) ([]int64, error)
	// InsertEmbeddings stores embeddings keyed by chunk ID.
	InsertEmbeddings(chunkIDs []int64, embeddings [][]float32) error
	// Search finds the top-k chunks closest to the query embedding.
	Search(queryEmbedding []float32, k int) ([]SearchResult, error)
	// GetMeta returns a metadata value by key, or "" if not set.
	GetMeta(key string) (string, error)
	// SetMeta sets a metadata key-value pair.
	SetMeta(key, value string) error
	// DeleteAllChunks removes all files, chunks, and embeddings.
	DeleteAllChunks() error
	// Close closes the underlying database.
	Close() error
}

// SQLiteStore implements Store backed by SQLite + sqlite-vec.
type SQLiteStore struct {
	db *sql.DB
}

// Open creates or opens a SQLite database at the given path and initializes the schema.
func Open(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := Init(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) GetFileHash(path string) (string, error) {
	var hash string
	err := s.db.QueryRow("SELECT hash FROM files WHERE path = ?", path).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return hash, err
}

func (s *SQLiteStore) UpsertFile(f FileRecord) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// Check if file exists.
	var existingID int64
	err = tx.QueryRow("SELECT id FROM files WHERE path = ?", f.Path).Scan(&existingID)
	if err == nil {
		// File exists — delete old chunks and embeddings.
		rows, err := tx.Query("SELECT id FROM chunks WHERE file_id = ?", existingID)
		if err != nil {
			return 0, err
		}
		var chunkIDs []int64
		for rows.Next() {
			var cid int64
			if err := rows.Scan(&cid); err != nil {
				rows.Close()
				return 0, err
			}
			chunkIDs = append(chunkIDs, cid)
		}
		rows.Close()

		for _, cid := range chunkIDs {
			if _, err := tx.Exec("DELETE FROM vec_chunks WHERE chunk_id = ?", cid); err != nil {
				return 0, err
			}
		}
		if _, err := tx.Exec("DELETE FROM chunks WHERE file_id = ?", existingID); err != nil {
			return 0, err
		}
		// Update the file record.
		_, err = tx.Exec(
			"UPDATE files SET hash = ?, language = ?, indexed_at = CURRENT_TIMESTAMP, size_bytes = ? WHERE id = ?",
			f.Hash, f.Language, f.SizeBytes, existingID,
		)
		if err != nil {
			return 0, err
		}
		if err := tx.Commit(); err != nil {
			return 0, err
		}
		return existingID, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}

	// Insert new file.
	res, err := tx.Exec(
		"INSERT INTO files (path, hash, language, size_bytes) VALUES (?, ?, ?, ?)",
		f.Path, f.Hash, f.Language, f.SizeBytes,
	)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *SQLiteStore) InsertChunks(fileID int64, chunks []Chunk) ([]int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(
		"INSERT INTO chunks (file_id, name, kind, start_line, end_line, content, metadata) VALUES (?, ?, ?, ?, ?, ?, ?)",
	)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	ids := make([]int64, 0, len(chunks))
	for _, c := range chunks {
		meta := c.Metadata
		if meta == "" {
			meta = "{}"
		}
		res, err := stmt.Exec(fileID, c.Name, c.Kind, c.StartLine, c.EndLine, c.Content, meta)
		if err != nil {
			return nil, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return ids, nil
}

func (s *SQLiteStore) InsertEmbeddings(chunkIDs []int64, embeddings [][]float32) error {
	if len(chunkIDs) != len(embeddings) {
		return fmt.Errorf("mismatched chunk IDs (%d) and embeddings (%d)", len(chunkIDs), len(embeddings))
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("INSERT INTO vec_chunks (chunk_id, embedding) VALUES (?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i, cid := range chunkIDs {
		blob, err := sqlite_vec.SerializeFloat32(embeddings[i])
		if err != nil {
			return fmt.Errorf("serialize embedding for chunk %d: %w", cid, err)
		}
		if _, err := stmt.Exec(cid, blob); err != nil {
			return fmt.Errorf("insert embedding for chunk %d: %w", cid, err)
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) Search(queryEmbedding []float32, k int) ([]SearchResult, error) {
	blob, err := sqlite_vec.SerializeFloat32(queryEmbedding)
	if err != nil {
		return nil, fmt.Errorf("serialize query embedding: %w", err)
	}
	rows, err := s.db.Query(`
		SELECT v.chunk_id, v.distance, c.name, c.kind, c.start_line, c.end_line, c.content, c.metadata,
		       f.path, f.language
		FROM vec_chunks v
		JOIN chunks c ON c.id = v.chunk_id
		JOIN files f ON f.id = c.file_id
		WHERE v.embedding MATCH ?
		ORDER BY v.distance
		LIMIT ?
	`, blob, k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		err := rows.Scan(
			&r.Chunk.ID, &r.Distance,
			&r.Chunk.Name, &r.Chunk.Kind, &r.Chunk.StartLine, &r.Chunk.EndLine,
			&r.Chunk.Content, &r.Chunk.Metadata,
			&r.FilePath, &r.Language,
		)
		if err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func (s *SQLiteStore) GetMeta(key string) (string, error) {
	var value string
	err := s.db.QueryRow("SELECT value FROM meta WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (s *SQLiteStore) SetMeta(key, value string) error {
	_, err := s.db.Exec(
		"INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		key, value,
	)
	return err
}

func (s *SQLiteStore) DeleteAllChunks() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM vec_chunks"); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM chunks"); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM files"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
