package languages

import (
	"synapse/internal/chunker"

	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

func RegisterTypeScript(r *chunker.Registry) {
	r.Register("typescript", &chunker.LanguageSpec{
		Language: typescript.GetLanguage(),
		Query: `
			(function_declaration name: (identifier) @name) @chunk
			(class_declaration name: (type_identifier) @name) @chunk
			(method_definition name: (property_identifier) @name) @chunk
			(export_statement (function_declaration name: (identifier) @name)) @chunk
			(export_statement (class_declaration name: (type_identifier) @name)) @chunk
			(lexical_declaration (variable_declarator name: (identifier) @name value: (arrow_function))) @chunk
			(interface_declaration name: (type_identifier) @name) @chunk
			(type_alias_declaration name: (type_identifier) @name) @chunk
		`,
		Extensions: []string{"ts", "tsx"},
	})
}
