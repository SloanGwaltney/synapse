package languages

import (
	"synapse/internal/chunker"

	"github.com/smacker/go-tree-sitter/python"
)

func RegisterPython(r *chunker.Registry) {
	r.Register("python", &chunker.LanguageSpec{
		Language: python.GetLanguage(),
		Query: `
			(function_definition name: (identifier) @name) @chunk
			(class_definition name: (identifier) @name) @chunk
			(decorated_definition definition: (function_definition name: (identifier) @name)) @chunk
			(decorated_definition definition: (class_definition name: (identifier) @name)) @chunk
		`,
		Extensions: []string{"py", "pyi"},
	})
}
