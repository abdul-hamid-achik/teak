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
			{Pattern: `^###.*$`, Type: GenericHeading},
			{Pattern: `^(>>>)(capture|shell|db|multipart|graphql|variables)\b([^\n]*)`, Type: ByGroups(KeywordReserved, KeywordReserved, Text)},
			{Pattern: `^(>>>)([^\n]*)`, Type: ByGroups(KeywordReserved, Text)},
			{Pattern: `^(<<<)(\s*)$`, Type: ByGroups(KeywordReserved, Text)},
			{Pattern: `^(#\s*)(@[A-Za-z][\w.-]*)(.*)$`, Type: ByGroups(CommentSingle, NameDecorator, CommentSingle)},
			{Pattern: `^(#.*|//.*)$`, Type: CommentSingle},
			{Pattern: `^(@[\w.-]+)(\s*)(=)`, Type: ByGroups(NameVariable, Text, Operator)},
			{Pattern: `^(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|TRACE|CONNECT|WS)(\s+)(\S+)`, Type: ByGroups(NameFunction, Text, NameTag)},
			{Pattern: `^([A-Za-z][\w-]*)(\s*)(:)(\s*)([^\n]+)`, Type: ByGroups(NameAttribute, Text, Operator, Text, LiteralString)},
			{Pattern: `^([?&])(\s*)([\w.-]+)(\s*)(=)(\s*)([^\n]+)`, Type: ByGroups(Operator, Text, NameAttribute, Text, Operator, Text, LiteralString)},
			{Pattern: `\{\{[^}]+\}\}`, Type: LiteralStringInterpol},
			{Pattern: Words(`\b`, `\b`, "expect"), Type: KeywordReserved},
			{Pattern: Words(`\b`, `\b`, "status", "body", "header", "jsonpath", "duration", "size", "p50", "p95", "p99"), Type: NameAttribute},
			{Pattern: `==|!=|>=|<=|>|<`, Type: Operator},
			{Pattern: Words(`\b`, `\b`, "contains", "!contains", "startsWith", "endsWith", "matches", "exists", "!exists", "length", "includes", "!includes", "in", "!in", "type", "each", "schema", "snapshot"), Type: OperatorWord},
			{Pattern: `"([^"\\]|\\.)*"`, Type: LiteralStringDouble},
			{Pattern: `'([^'\\]|\\.)*'`, Type: LiteralString},
			{Pattern: `\b(true|false|null)\b`, Type: KeywordConstant},
			{Pattern: `\b-?\d+(\.\d+)?\b`, Type: LiteralNumber},
			{Pattern: `[{}\[\](),]`, Type: Punctuation},
			{Pattern: `\s+`, Type: Text},
			{Pattern: `.`, Type: Text},
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
			{Pattern: `#.*$`, Type: CommentSingle},
			{Pattern: `@>`, Type: NameDecorator},
			{Pattern: `@(?=\s|")`, Type: NameDecorator},
			{Pattern: `<-|\|>|->`, Type: Operator},
			{Pattern: `"([^"\\]|\\.)*"`, Type: LiteralStringDouble},
			{Pattern: Words(`\b`, `\b`, "GET", "POST", "PUT", "PATCH", "DELETE", "STREAM", "WS"), Type: NameFunction},
			{Pattern: Words(`\b`, `\b`, "blueprint", "model", "content", "save", "fn", "pipe", "middleware", "worker", "schedule", "secret", "env", "locale", "translation", "type", "alias", "enum", "state", "analytics", "include", "external", "subscribe", "test", "fixture", "test_group"), Type: KeywordReserved},
			{Pattern: Words(`\b`, `\b`, "before", "after", "stream", "logic", "impl", "setup", "request", "expect", "cleanup", "on_error", "on_fail", "on_connect", "on_message", "on_disconnect"), Type: KeywordReserved},
			{Pattern: Words(`\b`, `\b`, "guard", "when", "try", "recover", "skip"), Type: KeywordReserved},
			{Pattern: Words(`\b`, `\b`, "fetch", "query", "save", "update", "delete", "count", "upload", "download", "emit", "call", "log", "map", "seed", "inject", "publish", "archive", "rollback", "import_bundle", "export_bundle", "join", "leave", "broadcast", "whisper", "close"), Type: NameBuiltin},
			{Pattern: Words(`\b`, `\b`, "string", "int", "float", "bool", "uuid", "timestamp", "json", "file", "money"), Type: KeywordType},
			{Pattern: Words(`\b`, `\b`, "required", "optional", "primary", "unique", "index", "default", "ref", "format", "min", "max", "auto"), Type: NameDecorator},
			{Pattern: Words(`\b`, `\b`, "where", "paginate", "order", "first", "asc", "desc", "and", "or", "not", "in", "from", "as", "with", "using"), Type: OperatorWord},
			{Pattern: Words(`\b`, `\b`, "true", "false", "null", "now", "node", "postgres", "redis", "s3", "mysql", "sqlite", "mongo", "memcached", "sqs", "rabbitmq", "gcs", "local", "bearer", "api_key", "session", "webhook_sig"), Type: KeywordConstant},
			{Pattern: `\b(image|video|audio|text|application)/(\*|[-+\w]+)\b`, Type: LiteralString},
			{Pattern: `:\w+`, Type: NameAttribute},
			{Pattern: `\b\d+/(min|hour|day)\b`, Type: LiteralNumber},
			{Pattern: `\b\d+(\.\d+)?(ms|s|min|hour|hours|day|days|kb|mb|gb|b)\b`, Type: LiteralNumber},
			{Pattern: `\b\d+(\.\d+)?\b`, Type: LiteralNumber},
			{Pattern: `[{}\[\](),.]`, Type: Punctuation},
			{Pattern: `\s+`, Type: Text},
			{Pattern: `.`, Type: Text},
		},
	}
}
