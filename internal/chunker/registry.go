package chunker

import (
	"path/filepath"
	"strings"
	"sync"

	sitter "github.com/smacker/go-tree-sitter"
)

// LanguageSpec defines the tree-sitter grammar and query for a language.
type LanguageSpec struct {
	Language *sitter.Language
	// Query is a tree-sitter S-expression query that captures top-level
	// definitions. It must use @chunk for the outer node and @name for the
	// identifier (optional).
	Query      string
	Extensions []string
}

// Registry maps file extensions to language specs.
type Registry struct {
	mu    sync.RWMutex
	specs map[string]*LanguageSpec // extension (without dot) → spec
	langs map[string]*LanguageSpec // language name → spec
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		specs: make(map[string]*LanguageSpec),
		langs: make(map[string]*LanguageSpec),
	}
}

// Register adds a language spec under the given name.
func (r *Registry) Register(name string, spec *LanguageSpec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.langs[name] = spec
	for _, ext := range spec.Extensions {
		r.specs[ext] = spec
	}
}

// Lookup returns the spec for a file path based on its extension, or nil.
func (r *Registry) Lookup(path string) (spec *LanguageSpec, lang string) {
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.specs[ext]
	if !ok {
		return nil, ""
	}
	// Find the language name for this spec.
	for name, sp := range r.langs {
		if sp == s {
			return s, name
		}
	}
	return s, ext
}

// LanguageName returns the language name for a file path, or "".
func (r *Registry) LanguageName(path string) string {
	_, lang := r.Lookup(path)
	return lang
}

// Extensions returns the set of all registered file extensions (without dot).
func (r *Registry) Extensions() map[string]bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	exts := make(map[string]bool, len(r.specs))
	for ext := range r.specs {
		exts[ext] = true
	}
	return exts
}
