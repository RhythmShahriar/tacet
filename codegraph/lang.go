// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package codegraph

import (
	"path/filepath"
	"strings"

	ts "github.com/odvcencio/gotreesitter"
	c "github.com/odvcencio/gotreesitter/grammars/c"
	csharp "github.com/odvcencio/gotreesitter/grammars/c_sharp"
	cpp "github.com/odvcencio/gotreesitter/grammars/cpp"
	golang "github.com/odvcencio/gotreesitter/grammars/go"
	java "github.com/odvcencio/gotreesitter/grammars/java"
	javascript "github.com/odvcencio/gotreesitter/grammars/javascript"
	kotlin "github.com/odvcencio/gotreesitter/grammars/kotlin"
	php "github.com/odvcencio/gotreesitter/grammars/php"
	python "github.com/odvcencio/gotreesitter/grammars/python"
	ruby "github.com/odvcencio/gotreesitter/grammars/ruby"
	grammarruntime "github.com/odvcencio/gotreesitter/grammars/runtime"
	rust "github.com/odvcencio/gotreesitter/grammars/rust"
	tsx "github.com/odvcencio/gotreesitter/grammars/tsx"
	typescript "github.com/odvcencio/gotreesitter/grammars/typescript"
)

// Only the standalone grammar packages are imported, never the aggregate
// grammars package: that one embeds all 200+ grammars (some GPL) in the
// binary. These are the languages tacet indexes and nothing else.

// language is one grammar and the query that finds its definitions.
type language struct {
	name  string
	load  func() *ts.Language
	query string
}

const jsDefs = `
(function_declaration name: (identifier) @name) @definition.function
(generator_function_declaration name: (identifier) @name) @definition.function
(method_definition name: (property_identifier) @name) @definition.method
(program (lexical_declaration (variable_declarator name: (identifier) @name value: [(arrow_function) (function_expression)]) @definition.function))
(export_statement (lexical_declaration (variable_declarator name: (identifier) @name value: [(arrow_function) (function_expression)]) @definition.function))
(program (variable_declaration (variable_declarator name: (identifier) @name value: [(arrow_function) (function_expression)]) @definition.function))
(program (lexical_declaration (variable_declarator name: (identifier) @name) @definition.constant))
(export_statement (lexical_declaration (variable_declarator name: (identifier) @name) @definition.constant))
`

const tsDefs = jsDefs + `
(class_declaration name: (type_identifier) @name) @definition.class
(abstract_class_declaration name: (type_identifier) @name) @definition.class
(interface_declaration name: (type_identifier) @name) @definition.interface
(type_alias_declaration name: (type_identifier) @name) @definition.type
(enum_declaration name: (identifier) @name) @definition.enum
`

const cDefs = `
(function_definition declarator: (function_declarator declarator: (_) @name)) @definition.function
(function_definition declarator: (pointer_declarator declarator: (function_declarator declarator: (_) @name))) @definition.function
(struct_specifier name: (type_identifier) @name body: (_)) @definition.struct
(enum_specifier name: (type_identifier) @name body: (_)) @definition.enum
(type_definition declarator: (type_identifier) @name) @definition.type
(preproc_function_def name: (identifier) @name) @definition.macro
`

var languages = map[string]*language{
	"go": {name: "go", load: golang.Language, query: `
(function_declaration name: (identifier) @name) @definition.function
(method_declaration name: (field_identifier) @name) @definition.method
(source_file (type_declaration (type_spec name: (type_identifier) @name type: (struct_type)) @definition.struct))
(source_file (type_declaration (type_spec name: (type_identifier) @name type: (interface_type)) @definition.interface))
(source_file (type_declaration (type_spec name: (type_identifier) @name) @definition.type))
(source_file (type_declaration (type_alias name: (type_identifier) @name) @definition.type))
(method_elem name: (field_identifier) @name) @definition.method
(source_file (const_declaration (const_spec name: (identifier) @name) @definition.constant))
(source_file (var_declaration (var_spec name: (identifier) @name) @definition.variable))
`},
	"typescript": {name: "typescript", load: typescript.Language, query: tsDefs},
	"tsx":        {name: "tsx", load: tsx.Language, query: tsDefs},
	"javascript": {name: "javascript", load: javascript.Language, query: jsDefs + `
(class_declaration name: (identifier) @name) @definition.class
`},
	"python": {name: "python", load: python.Language, query: `
(function_definition name: (identifier) @name) @definition.function
(class_definition name: (identifier) @name) @definition.class
`},
	"php": {name: "php", load: php.Language, query: `
(function_definition name: (name) @name) @definition.function
(method_declaration name: (name) @name) @definition.method
(class_declaration name: (name) @name) @definition.class
(interface_declaration name: (name) @name) @definition.interface
(trait_declaration name: (name) @name) @definition.trait
(enum_declaration name: (name) @name) @definition.enum
`},
	"rust": {name: "rust", load: rust.Language, query: `
(function_item name: (identifier) @name) @definition.function
(function_signature_item name: (identifier) @name) @definition.function
(struct_item name: (type_identifier) @name) @definition.struct
(enum_item name: (type_identifier) @name) @definition.enum
(trait_item name: (type_identifier) @name) @definition.trait
(type_item name: (type_identifier) @name) @definition.type
(const_item name: (identifier) @name) @definition.constant
(static_item name: (identifier) @name) @definition.constant
(macro_definition name: (identifier) @name) @definition.macro
(mod_item name: (identifier) @name) @definition.module
(impl_item type: (type_identifier) @name) @definition.impl
`},
	"java": {name: "java", load: java.Language, query: `
(class_declaration name: (identifier) @name) @definition.class
(interface_declaration name: (identifier) @name) @definition.interface
(enum_declaration name: (identifier) @name) @definition.enum
(record_declaration name: (identifier) @name) @definition.class
(annotation_type_declaration name: (identifier) @name) @definition.interface
(method_declaration name: (identifier) @name) @definition.method
(constructor_declaration name: (identifier) @name) @definition.constructor
`},
	"ruby": {name: "ruby", load: ruby.Language, query: `
(class name: (constant) @name) @definition.class
(module name: (constant) @name) @definition.module
(method name: (identifier) @name) @definition.method
(singleton_method name: (identifier) @name) @definition.method
`},
	"c": {name: "c", load: c.Language, query: cDefs},
	"cpp": {name: "cpp", load: cpp.Language, query: cDefs + `
(class_specifier name: (type_identifier) @name body: (_)) @definition.class
(namespace_definition name: (namespace_identifier) @name) @definition.module
`},
	"c_sharp": {name: "c_sharp", load: csharp.Language, query: `
(class_declaration name: (identifier) @name) @definition.class
(interface_declaration name: (identifier) @name) @definition.interface
(struct_declaration name: (identifier) @name) @definition.struct
(enum_declaration name: (identifier) @name) @definition.enum
(record_declaration name: (identifier) @name) @definition.class
(method_declaration name: (identifier) @name) @definition.method
(constructor_declaration name: (identifier) @name) @definition.constructor
`},
	"kotlin": {name: "kotlin", load: kotlin.Language, query: `
(class_declaration (type_identifier) @name) @definition.class
(object_declaration (type_identifier) @name) @definition.object
(function_declaration (simple_identifier) @name) @definition.function
(type_alias (type_identifier) @name) @definition.type
`},
}

// extensions maps a file extension to its language.
var extensions = map[string]string{
	".go": "go",
	".ts": "typescript", ".mts": "typescript", ".cts": "typescript",
	".tsx": "tsx",
	".js":  "javascript", ".jsx": "javascript", ".mjs": "javascript", ".cjs": "javascript",
	".py":   "python",
	".php":  "php",
	".rs":   "rust",
	".java": "java",
	".rb":   "ruby",
	".c":    "c", ".h": "c",
	".cc": "cpp", ".cpp": "cpp", ".cxx": "cpp", ".hpp": "cpp", ".hh": "cpp",
	".cs": "c_sharp",
	".kt": "kotlin", ".kts": "kotlin",
}

// langOf is the language of path, or nil when tacet doesn't index it.
func langOf(path string) *language {
	base := filepath.Base(path)
	if strings.Contains(base, ".min.") || strings.HasSuffix(base, ".d.ts") {
		return nil // minified bundles and generated declarations
	}
	return languages[extensions[strings.ToLower(filepath.Ext(base))]]
}

// Supported reports whether path is in a language the index reads.
func Supported(path string) bool { return langOf(path) != nil }

// tokenSource is the grammar's own lexer, or nil for the table lexer.
func (l *language) tokenSource() func([]byte, *ts.Language) ts.TokenSource {
	return grammarruntime.TokenSourceFactory(l.name)
}

// identTypes are the leaf node types that can name a symbol.
var identTypes = map[string]bool{
	"identifier": true, "type_identifier": true, "field_identifier": true,
	"property_identifier": true, "shorthand_property_identifier": true,
	"name": true, "constant": true, "simple_identifier": true,
	"namespace_identifier": true,
}

// memberTypes are the nodes for a.b, a::b and a->b.
var memberTypes = map[string]bool{
	"selector_expression": true, "member_expression": true, "attribute": true,
	"field_expression": true, "scoped_identifier": true, "field_access": true,
	"member_access_expression": true, "navigation_expression": true,
	"qualified_identifier": true, "scope_resolution": true, "navigation_suffix": true,
}

// callTypes are the nodes that call or construct what comes first in them.
var callTypes = map[string]bool{
	"call_expression": true, "call": true, "method_invocation": true,
	"function_call_expression": true, "member_call_expression": true,
	"nullsafe_member_call_expression": true, "scoped_call_expression": true,
	"new_expression": true, "object_creation_expression": true,
	"invocation_expression": true, "macro_invocation": true,
	"struct_expression": true, "composite_literal": true,
	"jsx_opening_element": true, "jsx_self_closing_element": true,
	"decorator": true,
}

// typeRefTypes are leaves that always name a type.
var typeRefTypes = map[string]bool{"type_identifier": true}

// families group languages whose code can call each other's symbols, so
// a Go file never counts as a use of a PHP function with the same name.
var families = map[string]string{
	"typescript": "js", "tsx": "js", "javascript": "js",
	"java": "jvm", "kotlin": "jvm", "c": "c", "cpp": "c",
}

// family is the group a language belongs to.
func family(lang string) string {
	if f, ok := families[lang]; ok {
		return f
	}
	return lang
}

// builtins are names every file of a family uses without defining them.
// A project symbol that shares one gets no rank from those uses.
var builtins = map[string]map[string]bool{
	"go": words(`append cap clear close complex copy delete imag len make max min new panic print println
real recover any bool byte comparable error float32 float64 int int8 int16 int32 int64 rune string
uint uint8 uint16 uint32 uint64 uintptr iota nil true false Error String`),
	"js": words(`console log error warn Object Array String Number Boolean Promise JSON Math Date Error Map Set
Symbol RegExp parseInt parseFloat require exports module undefined window document fetch setTimeout
push map filter reduce forEach then catch length toString includes keys values entries join split
slice replace trim find some every indexOf concat sort assign Partial Record Pick Omit Readonly Required
Exclude Extract NonNullable ReturnType Parameters Awaited InstanceType`),
	"python": words(`print len str int float bool list dict set tuple range enumerate zip isinstance super self cls
type object open Exception ValueError TypeError KeyError None True False append get items keys values
format join split strip`),
	"php": words(`array count isset empty unset strlen sprintf implode explode in_array is_array is_string
array_map array_merge array_keys json_encode json_decode this self parent static null true false`),
	"rust": words(`Some None Ok Err Box Vec String Option Result Self self println format vec new clone unwrap
into iter map collect len push to_string as_str expect`),
	"jvm": words(`String Object Integer Long Boolean List Map Set ArrayList HashMap System out println
equals hashCode toString get set add size this super null`),
}

func words(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}
