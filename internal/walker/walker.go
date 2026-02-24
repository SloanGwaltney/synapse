package walker

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// FileInfo holds metadata about a discovered source file.
type FileInfo struct {
	Path    string
	RelPath string
	Size    int64
}

// maxFileSize is the largest file we'll consider (1 MB).
const maxFileSize = 1 << 20

// defaultIgnores are used when no .synapseignore file exists.
var defaultIgnores = []string{
	".git",
	".svn",
	".hg",
	"node_modules",
	"vendor",
	"__pycache__",
	".idea",
	".vscode",
	".synapse",
	"dist",
	"build",
}

// Walk traverses the directory tree rooted at root and sends discovered
// source files on the returned channel. It only emits files whose extension
// is in allowedExts, and skips directories matching .synapseignore patterns.
func Walk(root string, allowedExts map[string]bool) (<-chan FileInfo, <-chan error) {
	files := make(chan FileInfo, 64)
	errs := make(chan error, 1)

	go func() {
		defer close(files)
		defer close(errs)

		absRoot, err := filepath.Abs(root)
		if err != nil {
			errs <- err
			return
		}

		ignores := loadIgnorePatterns(absRoot)

		err = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // skip errors, keep walking
			}

			if d.IsDir() {
				if path == absRoot {
					return nil
				}
				rel, _ := filepath.Rel(absRoot, path)
				name := d.Name()
				if matchesIgnore(name, filepath.ToSlash(rel), ignores) {
					return filepath.SkipDir
				}
				return nil
			}

			// Skip symlinks.
			if d.Type()&fs.ModeSymlink != 0 {
				return nil
			}

			// Only process files with registered extensions.
			ext := strings.TrimPrefix(filepath.Ext(path), ".")
			if !allowedExts[ext] {
				return nil
			}

			info, err := d.Info()
			if err != nil {
				return nil
			}

			// Skip large or empty files.
			if info.Size() > maxFileSize || info.Size() == 0 {
				return nil
			}

			relPath, _ := filepath.Rel(absRoot, path)
			files <- FileInfo{
				Path:    path,
				RelPath: filepath.ToSlash(relPath),
				Size:    info.Size(),
			}
			return nil
		})
		if err != nil {
			errs <- err
		}
	}()

	return files, errs
}

// loadIgnorePatterns reads .synapseignore from the project root.
// If the file doesn't exist, it creates one with the default patterns.
func loadIgnorePatterns(root string) []string {
	ignorePath := filepath.Join(root, ".synapseignore")

	f, err := os.Open(ignorePath)
	if err != nil {
		// File doesn't exist — create it with defaults.
		createDefaultIgnoreFile(ignorePath)
		return defaultIgnores
	}
	defer f.Close()

	var patterns []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	if len(patterns) == 0 {
		return defaultIgnores
	}
	return patterns
}

func createDefaultIgnoreFile(path string) {
	var b strings.Builder
	b.WriteString("# Directories to exclude from indexing.\n")
	b.WriteString("# One pattern per line. Supports exact names and globs.\n\n")
	for _, p := range defaultIgnores {
		b.WriteString(p)
		b.WriteByte('\n')
	}
	// Best-effort write; if it fails the defaults are still used in memory.
	os.WriteFile(path, []byte(b.String()), 0o644)
}

// matchesIgnore checks if a directory name or relative path matches any ignore pattern.
func matchesIgnore(name, relPath string, patterns []string) bool {
	for _, p := range patterns {
		// Exact directory name match (e.g. "node_modules", ".git").
		if name == p {
			return true
		}
		// Path prefix match (e.g. "third_party/vendor").
		if strings.HasPrefix(relPath, p) {
			return true
		}
		// Glob match against the relative path.
		if matched, _ := filepath.Match(p, relPath); matched {
			return true
		}
		if matched, _ := filepath.Match(p, name); matched {
			return true
		}
	}
	return false
}
