package store

import (
	"database/sql"
	"strings"
)

const ddl = `
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;

CREATE TABLE IF NOT EXISTS files (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    path       TEXT NOT NULL UNIQUE,
    hash       TEXT NOT NULL,
    language   TEXT NOT NULL DEFAULT '',
    indexed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    size_bytes INTEGER NOT NULL DEFAULT 0,
    summary    TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS chunks (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    file_id    INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    name       TEXT NOT NULL DEFAULT '',
    kind       TEXT NOT NULL DEFAULT '',
    start_line INTEGER NOT NULL,
    end_line   INTEGER NOT NULL,
    content    TEXT NOT NULL,
    metadata   TEXT NOT NULL DEFAULT '{}'
);

CREATE VIRTUAL TABLE IF NOT EXISTS vec_chunks USING vec0(
    chunk_id INTEGER PRIMARY KEY,
    embedding float[768]
);

CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
    name, content, content=chunks, content_rowid=id
);

CREATE TRIGGER IF NOT EXISTS chunks_ai AFTER INSERT ON chunks BEGIN
    INSERT INTO chunks_fts(rowid, name, content) VALUES (new.id, new.name, new.content);
END;

CREATE TRIGGER IF NOT EXISTS chunks_ad AFTER DELETE ON chunks BEGIN
    INSERT INTO chunks_fts(chunks_fts, rowid, name, content) VALUES('delete', old.id, old.name, old.content);
END;
`

// Init creates the schema tables if they don't exist.
func Init(db *sql.DB) error {
	if _, err := db.Exec(ddl); err != nil {
		return err
	}
	// Migration: add summary column for existing databases.
	_, err := db.Exec("ALTER TABLE files ADD COLUMN summary TEXT NOT NULL DEFAULT ''")
	if err != nil && !isDuplicateColumn(err) {
		return err
	}
	return nil
}

func isDuplicateColumn(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate column")
}
