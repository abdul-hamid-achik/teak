package highlight

import (
	"testing"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

func TestHitspecLexerRegistered(t *testing.T) {
	lexer := lexers.Match("api.hitspec")
	if lexer == nil {
		t.Fatal("expected lexer for .hitspec file")
	}
	if lexer.Config().Name != "hitspec" {
		t.Fatalf("lexer name = %q, want %q", lexer.Config().Name, "hitspec")
	}
}

func TestHitspecLexerTokenisesCoreSyntax(t *testing.T) {
	lexer := lexers.Match("api.http")
	if lexer == nil {
		t.Fatal("expected lexer for .http file")
	}

	src := `@baseUrl = https://api.example.com
### Health
# @name health
GET {{baseUrl}}/health
Content-Type: application/json

>>> 
expect status == 200
<<<
`

	tokens, err := chroma.Tokenise(lexer, nil, src)
	if err != nil {
		t.Fatalf("Tokenise() error = %v", err)
	}

	assertHasToken(t, tokens, chroma.NameVariable, "@baseUrl")
	assertHasToken(t, tokens, chroma.GenericHeading, "### Health")
	assertHasToken(t, tokens, chroma.NameFunction, "GET")
	assertHasToken(t, tokens, chroma.NameAttribute, "Content-Type")
	assertHasToken(t, tokens, chroma.KeywordReserved, "expect")
	assertHasToken(t, tokens, chroma.NameAttribute, "status")
}

func TestBlueprintLexerRegistered(t *testing.T) {
	lexer := lexers.Match("service.bp")
	if lexer == nil {
		t.Fatal("expected lexer for .bp file")
	}
	if lexer.Config().Name != "Blueprint" {
		t.Fatalf("lexer name = %q, want %q", lexer.Config().Name, "Blueprint")
	}
}

func TestBlueprintLexerTokenisesCoreSyntax(t *testing.T) {
	lexer := lexers.Match("service.bp")
	if lexer == nil {
		t.Fatal("expected lexer for .bp file")
	}

	src := `blueprint "todo-api" {
  version "1.0.0"
}

# comment
@ "Create a todo"
POST /api/todos {
  <- title string required
  |> todo = save todo { title: title }
  -> 201 { id: todo.id }
}
`

	tokens, err := chroma.Tokenise(lexer, nil, src)
	if err != nil {
		t.Fatalf("Tokenise() error = %v", err)
	}

	assertHasToken(t, tokens, chroma.KeywordReserved, "blueprint")
	assertHasToken(t, tokens, chroma.CommentSingle, "# comment")
	assertHasToken(t, tokens, chroma.NameDecorator, "@")
	assertHasToken(t, tokens, chroma.NameFunction, "POST")
	assertHasToken(t, tokens, chroma.Operator, "<-")
	assertHasToken(t, tokens, chroma.Operator, "|>")
	assertHasToken(t, tokens, chroma.Operator, "->")
	assertHasToken(t, tokens, chroma.KeywordType, "string")
}

func assertHasToken(t *testing.T, tokens []chroma.Token, wantType chroma.TokenType, wantValue string) {
	t.Helper()
	for _, tok := range tokens {
		if tok.Type == wantType && tok.Value == wantValue {
			return
		}
	}
	t.Fatalf("missing token type=%v value=%q in %#v", wantType, wantValue, tokens)
}
