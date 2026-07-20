package search

import (
	"testing"
)

func TestCompilePatternLiteral(t *testing.T) {
	re, err := CompilePattern("foo.bar", SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if !re.MatchString("foo.bar") {
		t.Error("literal pattern should match exact text")
	}
	if re.MatchString("fooXbar") {
		t.Error("literal pattern should not match regex metacharacters")
	}
}

func TestCompilePatternRegex(t *testing.T) {
	re, err := CompilePattern("foo.bar", SearchOpts{Regex: true})
	if err != nil {
		t.Fatal(err)
	}
	if !re.MatchString("fooXbar") {
		t.Error("regex pattern should match with metacharacters")
	}
	if !re.MatchString("foo.bar") {
		t.Error("regex pattern should also match literal dot")
	}
}

func TestCompilePatternCaseInsensitive(t *testing.T) {
	re, err := CompilePattern("hello", SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if !re.MatchString("HELLO") {
		t.Error("default should be case-insensitive")
	}
}

func TestCompilePatternCaseSensitive(t *testing.T) {
	re, err := CompilePattern("hello", SearchOpts{CaseSensitive: true})
	if err != nil {
		t.Fatal(err)
	}
	if re.MatchString("HELLO") {
		t.Error("case-sensitive should not match different case")
	}
	if !re.MatchString("hello") {
		t.Error("case-sensitive should match exact case")
	}
}

func TestCompilePatternInvalidRegex(t *testing.T) {
	_, err := CompilePattern("[invalid", SearchOpts{Regex: true})
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestCompilePatternRegexGroups(t *testing.T) {
	re, err := CompilePattern(`(\w+)@(\w+)`, SearchOpts{Regex: true})
	if err != nil {
		t.Fatal(err)
	}
	match := re.FindStringSubmatch("user@host")
	if len(match) != 3 || match[1] != "user" || match[2] != "host" {
		t.Errorf("expected groups [user host], got %v", match)
	}
}
