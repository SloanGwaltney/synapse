package languages

import (
	"synapse/internal/chunker"

	"github.com/smacker/go-tree-sitter/javascript"
)

func RegisterJavaScript(r *chunker.Registry) {
	r.Register("javascript", &chunker.LanguageSpec{
		Language: javascript.GetLanguage(),
		Query: `
			(function_declaration name: (identifier) @name) @chunk
			(class_declaration name: (identifier) @name) @chunk
			(method_definition name: (property_identifier) @name) @chunk
			(export_statement (function_declaration name: (identifier) @name)) @chunk
			(export_statement (class_declaration name: (identifier) @name)) @chunk
			(lexical_declaration (variable_declarator name: (identifier) @name value: (arrow_function))) @chunk
		`,
		Extensions: []string{"js", "jsx", "mjs", "cjs"},
	})
}
