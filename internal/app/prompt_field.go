package app

import "unicode/utf8"

func clampPromptCursor(text string, cursor int) int {
	if cursor < 0 {
		return 0
	}
	if cursor > len(text) {
		return len(text)
	}
	if cursor == 0 || cursor == len(text) {
		return cursor
	}
	_, size := utf8.DecodeRuneInString(text[cursor:])
	if size == 0 {
		return len(text)
	}
	return cursor
}

func setPrompt(text *string, cursor *int, value string) {
	*text = value
	*cursor = len(value)
}

func promptInsert(text *string, cursor *int, insert string) {
	if insert == "" {
		return
	}
	*cursor = clampPromptCursor(*text, *cursor)
	*text = (*text)[:*cursor] + insert + (*text)[*cursor:]
	*cursor += len(insert)
}

func promptBackspace(text *string, cursor *int) {
	*cursor = clampPromptCursor(*text, *cursor)
	if *cursor == 0 {
		return
	}
	_, size := utf8.DecodeLastRuneInString((*text)[:*cursor])
	start := *cursor - size
	*text = (*text)[:start] + (*text)[*cursor:]
	*cursor = start
}

func promptDelete(text *string, cursor *int) {
	*cursor = clampPromptCursor(*text, *cursor)
	if *cursor >= len(*text) {
		return
	}
	_, size := utf8.DecodeRuneInString((*text)[*cursor:])
	*text = (*text)[:*cursor] + (*text)[*cursor+size:]
}

func promptLeft(text *string, cursor *int) {
	*cursor = clampPromptCursor(*text, *cursor)
	if *cursor == 0 {
		return
	}
	_, size := utf8.DecodeLastRuneInString((*text)[:*cursor])
	*cursor -= size
}

func promptRight(text *string, cursor *int) {
	*cursor = clampPromptCursor(*text, *cursor)
	if *cursor >= len(*text) {
		return
	}
	_, size := utf8.DecodeRuneInString((*text)[*cursor:])
	*cursor += size
}

func promptHome(_ *string, cursor *int) {
	*cursor = 0
}

func promptEnd(text *string, cursor *int) {
	*cursor = len(*text)
}

func promptWithCaret(text string, cursor int) string {
	cursor = clampPromptCursor(text, cursor)
	return text[:cursor] + "_" + text[cursor:]
}

// applyPromptNav handles caret movement and deletion. It reports whether the
// key was consumed so callers can still accept filtered inserts.
func applyPromptNav(text *string, cursor *int, key string) bool {
	switch key {
	case "left":
		promptLeft(text, cursor)
		return true
	case "right":
		promptRight(text, cursor)
		return true
	case "home":
		promptHome(text, cursor)
		return true
	case "end":
		promptEnd(text, cursor)
		return true
	case "backspace":
		promptBackspace(text, cursor)
		return true
	case "delete":
		promptDelete(text, cursor)
		return true
	}
	return false
}
