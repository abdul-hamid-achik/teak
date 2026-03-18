package highlight

import (
	"github.com/alecthomas/chroma/v2/lexers"

	. "github.com/alecthomas/chroma/v2" // nolint
)

func init() {
	lexers.Register(newHitspecLexer())
	lexers.Register(newBlueprintLexer())
}

func newHitspecLexer() Lexer {
	return MustNewLexer(&Config{
		Name:            "hitspec",
		Aliases:         []string{"hitspec"},
		Filenames:       []string{"*.http", "*.hitspec"},
		MimeTypes:       []string{"text/x-hitspec"},
		CaseInsensitive: true,
		EnsureNL:        true,
		Priority:        10,
	}, hitspecRules)
}

func hitspecRules() Rules {
	return Rules{
		"root": {
			{`^###.*$`, GenericHeading, nil},
			{`^(>>>)(capture|shell|db|multipart|graphql|variables)\b([^\n]*)`, ByGroups(KeywordReserved, KeywordReserved, Text), nil},
			{`^(>>>)([^\n]*)`, ByGroups(KeywordReserved, Text), nil},
			{`^(<<<)(\s*)$`, ByGroups(KeywordReserved, Text), nil},
			{`^(#\s*)(@[A-Za-z][\w.-]*)(.*)$`, ByGroups(CommentSingle, NameDecorator, CommentSingle), nil},
			{`^(#.*|//.*)$`, CommentSingle, nil},
			{`^(@[\w.-]+)(\s*)(=)`, ByGroups(NameVariable, Text, Operator), nil},
			{`^(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|TRACE|CONNECT|WS)(\s+)(\S+)`, ByGroups(NameFunction, Text, NameTag), nil},
			{`^([A-Za-z][\w-]*)(\s*)(:)(\s*)([^\n]+)`, ByGroups(NameAttribute, Text, Operator, Text, LiteralString), nil},
			{`^([?&])(\s*)([\w.-]+)(\s*)(=)(\s*)([^\n]+)`, ByGroups(Operator, Text, NameAttribute, Text, Operator, Text, LiteralString), nil},
			{`\{\{[^}]+\}\}`, LiteralStringInterpol, nil},
			{Words(`\b`, `\b`, "expect"), KeywordReserved, nil},
			{Words(`\b`, `\b`, "status", "body", "header", "jsonpath", "duration", "size", "p50", "p95", "p99"), NameAttribute, nil},
			{`==|!=|>=|<=|>|<`, Operator, nil},
			{Words(`\b`, `\b`, "contains", "!contains", "startsWith", "endsWith", "matches", "exists", "!exists", "length", "includes", "!includes", "in", "!in", "type", "each", "schema", "snapshot"), OperatorWord, nil},
			{`"([^"\\]|\\.)*"`, LiteralStringDouble, nil},
			{`'([^'\\]|\\.)*'`, LiteralString, nil},
			{`\b(true|false|null)\b`, KeywordConstant, nil},
			{`\b-?\d+(\.\d+)?\b`, LiteralNumber, nil},
			{`[{}\[\](),]`, Punctuation, nil},
			{`\s+`, Text, nil},
			{`.`, Text, nil},
		},
	}
}

func newBlueprintLexer() Lexer {
	return MustNewLexer(&Config{
		Name:      "Blueprint",
		Aliases:   []string{"blueprint", "bp"},
		Filenames: []string{"*.bp"},
		MimeTypes: []string{"text/x-blueprint"},
		EnsureNL:  true,
		Priority:  10,
	}, blueprintRules)
}

func blueprintRules() Rules {
	return Rules{
		"root": {
			{`#.*$`, CommentSingle, nil},
			{`@>`, NameDecorator, nil},
			{`@(?=\s|")`, NameDecorator, nil},
			{`<-|\|>|->`, Operator, nil},
			{`"([^"\\]|\\.)*"`, LiteralStringDouble, nil},
			{Words(`\b`, `\b`, "GET", "POST", "PUT", "PATCH", "DELETE", "STREAM", "WS"), NameFunction, nil},
			{Words(`\b`, `\b`, "blueprint", "model", "content", "save", "fn", "pipe", "middleware", "worker", "schedule", "secret", "env", "locale", "translation", "type", "alias", "enum", "state", "analytics", "include", "external", "subscribe", "test", "fixture", "test_group"), KeywordReserved, nil},
			{Words(`\b`, `\b`, "before", "after", "stream", "logic", "impl", "setup", "request", "expect", "cleanup", "on_error", "on_fail", "on_connect", "on_message", "on_disconnect"), KeywordReserved, nil},
			{Words(`\b`, `\b`, "guard", "when", "try", "recover", "skip"), KeywordReserved, nil},
			{Words(`\b`, `\b`, "fetch", "query", "save", "update", "delete", "count", "upload", "download", "emit", "call", "log", "map", "seed", "inject", "publish", "archive", "rollback", "import_bundle", "export_bundle", "join", "leave", "broadcast", "whisper", "close"), NameBuiltin, nil},
			{Words(`\b`, `\b`, "string", "int", "float", "bool", "uuid", "timestamp", "json", "file", "money"), KeywordType, nil},
			{Words(`\b`, `\b`, "required", "optional", "primary", "unique", "index", "default", "ref", "format", "min", "max", "auto"), NameDecorator, nil},
			{Words(`\b`, `\b`, "where", "paginate", "order", "first", "asc", "desc", "and", "or", "not", "in", "from", "as", "with", "using"), OperatorWord, nil},
			{Words(`\b`, `\b`, "true", "false", "null", "now", "node", "postgres", "redis", "s3", "mysql", "sqlite", "mongo", "memcached", "sqs", "rabbitmq", "gcs", "local", "bearer", "api_key", "session", "webhook_sig"), KeywordConstant, nil},
			{`\b(image|video|audio|text|application)/(\*|[-+\w]+)\b`, LiteralString, nil},
			{`:\w+`, NameAttribute, nil},
			{`\b\d+/(min|hour|day)\b`, LiteralNumber, nil},
			{`\b\d+(\.\d+)?(ms|s|min|hour|hours|day|days|kb|mb|gb|b)\b`, LiteralNumber, nil},
			{`\b\d+(\.\d+)?\b`, LiteralNumber, nil},
			{`[{}\[\](),.]`, Punctuation, nil},
			{`\s+`, Text, nil},
			{`.`, Text, nil},
		},
	}
}
