package languages

import (
	"synapse/internal/chunker"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
)

func RegisterGo(r *chunker.Registry) {
	r.Register("go", &chunker.LanguageSpec{
		Language: golang.GetLanguage(),
		Query: `
			(function_declaration name: (identifier) @name) @chunk
			(method_declaration name: (field_identifier) @name) @chunk
			(type_declaration (type_spec name: (type_identifier) @name)) @chunk
		`,
		Extensions: []string{"go"},
	})
}

// Ensure *sitter.Language satisfies usage at compile time.
var _ *sitter.Language = golang.GetLanguage()
