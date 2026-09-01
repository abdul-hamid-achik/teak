package app

import "testing"

func TestPromptInsertAndArrows(t *testing.T) {
	text := "ab"
	cursor := len(text)
	promptLeft(&text, &cursor)
	promptInsert(&text, &cursor, "X")
	if text != "aXb" || cursor != 2 {
		t.Fatalf("insert in middle = %q cursor %d", text, cursor)
	}
	promptHome(&text, &cursor)
	promptInsert(&text, &cursor, "Z")
	if text != "ZaXb" || cursor != 1 {
		t.Fatalf("insert at home = %q cursor %d", text, cursor)
	}
	promptEnd(&text, &cursor)
	promptBackspace(&text, &cursor)
	if text != "ZaX" {
		t.Fatalf("backspace at end = %q", text)
	}
}

func TestPromptUnicodeBackspace(t *testing.T) {
	text := "rénamé"
	cursor := len(text)
	promptBackspace(&text, &cursor)
	if text != "rénam" {
		t.Fatalf("unicode backspace = %q, want rénam", text)
	}
}

func TestPromptWithCaret(t *testing.T) {
	if got := promptWithCaret("hi", 1); got != "h_i" {
		t.Fatalf("caret = %q", got)
	}
}
