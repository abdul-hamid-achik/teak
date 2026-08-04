package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"teak/internal/atomicfile"
)

// SaveOutcome reports how SaveTo persisted a config file.
type SaveOutcome int

const (
	// SavedRewritten means the file was created or re-encoded from scratch.
	SavedRewritten SaveOutcome = iota
	// SavedPatched means only the settings-UI keys were rewritten in place;
	// comments, formatting, and unknown sections survived.
	SavedPatched
	// SavedWithBackup means the file had to be re-encoded, and the original
	// was preserved at path + ".bak" first.
	SavedWithBackup
)

// SaveTo writes cfg atomically to path with private permissions. It is
// exported for callers that need an explicit path (including the Settings UI)
// and for tests; ordinary application code should use Save.
func SaveTo(path string, cfg Config) error {
	_, err := SaveToWithOutcome(path, cfg)
	return err
}

// SaveToWithOutcome is SaveTo plus a report of how the file was written. A
// pre-existing file is patched in place when possible so user comments and
// hand-written sections survive a Settings save; when a safe patch cannot be
// guaranteed the original is backed up before a full rewrite.
func SaveToWithOutcome(path string, cfg Config) (SaveOutcome, error) {
	if err := cfg.Validate(); err != nil {
		return SavedRewritten, fmt.Errorf("invalid config: %w", err)
	}
	if path == "" {
		return SavedRewritten, fmt.Errorf("config path is empty")
	}

	outcome := SavedRewritten
	existing, readErr := os.ReadFile(path)
	if readErr == nil {
		if patched, ok := patchConfigFile(string(existing), cfg); ok {
			if err := atomicfile.Write(path, func(file *os.File) error {
				_, err := file.WriteString(patched)
				return err
			}); err != nil {
				return SavedRewritten, fmt.Errorf("save config: %w", err)
			}
			return SavedPatched, nil
		}
		// The file cannot be patched safely. Keep the annotated original
		// before rewriting it so nothing the user wrote is unrecoverable.
		if err := os.WriteFile(path+".bak", existing, 0o600); err == nil {
			outcome = SavedWithBackup
		}
	}

	if err := atomicfile.Write(path, func(file *os.File) error {
		return toml.NewEncoder(file).Encode(cfg)
	}); err != nil {
		return outcome, fmt.Errorf("save config: %w", err)
	}
	return outcome, nil
}

// keyEdit is one settings-UI key that patchConfigFile rewrites.
type keyEdit struct {
	section string
	key     string
	value   string
	done    bool
}

// managedKeyEdits lists exactly the keys the Settings UI can change. Anything
// else in the file is preserved verbatim.
func managedKeyEdits(cfg Config) []*keyEdit {
	return []*keyEdit{
		{section: "editor", key: "tab_size", value: strconv.Itoa(cfg.Editor.TabSize)},
		{section: "editor", key: "insert_tabs", value: strconv.FormatBool(cfg.Editor.InsertTabs)},
		{section: "editor", key: "auto_indent", value: strconv.FormatBool(cfg.Editor.AutoIndent)},
		{section: "editor", key: "format_on_save", value: strconv.FormatBool(cfg.Editor.FormatOnSave)},
		{section: "editor", key: "word_wrap", value: strconv.FormatBool(cfg.Editor.WordWrap)},
		{section: "editor", key: "scroll_margin", value: strconv.Itoa(cfg.Editor.ScrollMargin)},
		{section: "ui", key: "theme", value: strconv.Quote(cfg.UI.Theme)},
		{section: "ui", key: "show_tree", value: strconv.FormatBool(cfg.UI.ShowTree)},
		{section: "ui", key: "tree_width", value: strconv.Itoa(cfg.UI.TreeWidth)},
	}
}

// patchConfigFile rewrites only the managed keys inside an existing config
// file, preserving comments, unknown sections, and formatting. It returns
// ok=false when it cannot guarantee a correct patch; the caller then falls
// back to a backed-up rewrite. Every successful result is re-parsed and the
// managed values re-checked before being accepted.
func patchConfigFile(existing string, cfg Config) (string, bool) {
	edits := managedKeyEdits(cfg)
	lines := strings.Split(existing, "\n")

	section := ""
	// sectionEnd maps a section name to the index just past its last content
	// line, used as the insertion point for managed keys not present yet.
	sectionEnd := make(map[string]int)
	sectionSeen := make(map[string]bool)

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			name, ok := parseSectionHeader(trimmed)
			if !ok {
				return "", false
			}
			if section != "" || i > 0 {
				if _, recorded := sectionEnd[section]; !recorded {
					sectionEnd[section] = i
				}
			}
			sectionEnd[section] = i
			section = name
			sectionSeen[name] = true
			continue
		}
		for _, edit := range edits {
			if edit.done || edit.section != section {
				continue
			}
			if isKeyLine(trimmed, edit.key) {
				lines[i] = rebuildKeyLine(line, edit.key, edit.value)
				edit.done = true
			}
		}
	}
	sectionEnd[section] = len(lines)

	// Append managed keys the file does not have: inside their section when it
	// exists, otherwise in a fresh section at the end.
	insertions := make(map[int][]string)
	var tail []string
	inTail := make(map[string]bool)
	for _, edit := range edits {
		if edit.done {
			continue
		}
		if sectionSeen[edit.section] {
			at := sectionEnd[edit.section]
			insertions[at] = append(insertions[at], fmt.Sprintf("%s = %s", edit.key, edit.value))
			continue
		}
		if !inTail[edit.section] {
			if len(tail) > 0 {
				tail = append(tail, "")
			}
			tail = append(tail, "["+edit.section+"]")
			inTail[edit.section] = true
		}
		tail = append(tail, fmt.Sprintf("%s = %s", edit.key, edit.value))
	}
	if len(insertions) > 0 {
		lines = applyInsertions(lines, insertions)
	}
	if len(tail) > 0 {
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		lines = append(lines, tail...)
	}

	patched := strings.Join(lines, "\n")
	if !patchRoundTrips(patched, cfg) {
		return "", false
	}
	return patched, true
}

// parseSectionHeader extracts a plain [section] name. Array-of-tables headers
// and anything unexpected report ok=false so the patch aborts rather than
// writing into the wrong scope.
func parseSectionHeader(trimmed string) (string, bool) {
	if strings.HasPrefix(trimmed, "[[") {
		return "", false
	}
	end := strings.Index(trimmed, "]")
	if end <= 1 {
		return "", false
	}
	name := strings.TrimSpace(trimmed[1:end])
	if name == "" || strings.ContainsAny(name, "[]#") {
		return "", false
	}
	if rest := strings.TrimSpace(trimmed[end+1:]); rest != "" && !strings.HasPrefix(rest, "#") {
		return "", false
	}
	return name, true
}

// isKeyLine reports whether trimmed is an assignment of key, e.g. "key = v".
// The comparison requires the next non-blank character after the key to be
// '=' so tab_size does not match tab_size_extra.
func isKeyLine(trimmed, key string) bool {
	if strings.HasPrefix(trimmed, `"`) {
		// Quoted keys are left alone; the caller falls back to a backup.
		return false
	}
	if !strings.HasPrefix(trimmed, key) {
		return false
	}
	rest := strings.TrimLeft(trimmed[len(key):], " \t")
	return strings.HasPrefix(rest, "=")
}

// rebuildKeyLine replaces the value of a key line while keeping its leading
// indentation and trailing comment.
func rebuildKeyLine(original, key, value string) string {
	leading := original[:len(original)-len(strings.TrimLeft(original, " \t"))]
	eq := strings.Index(original, "=")
	if eq < 0 {
		return leading + key + " = " + value
	}
	rest := original[eq+1:]
	trailing := ""
	if hash := strings.Index(rest, "#"); hash >= 0 {
		trailing = " " + strings.TrimSpace(rest[hash:])
	}
	return leading + key + " = " + value + trailing
}

// applyInsertions splices new lines into lines at the given indices.
func applyInsertions(lines []string, insertions map[int][]string) []string {
	out := make([]string, 0, len(lines)+4)
	for i, line := range lines {
		if adds, ok := insertions[i]; ok {
			out = append(out, adds...)
		}
		out = append(out, line)
	}
	if adds, ok := insertions[len(lines)]; ok {
		out = append(out, adds...)
	}
	return out
}

// patchRoundTrips re-parses the patched content and verifies every managed
// value landed. A mismatch means the patch would misrepresent the config, so
// the caller must fall back.
func patchRoundTrips(patched string, cfg Config) bool {
	var check Config
	if _, err := toml.Decode(patched, &check); err != nil {
		return false
	}
	return check.Editor.TabSize == cfg.Editor.TabSize &&
		check.Editor.InsertTabs == cfg.Editor.InsertTabs &&
		check.Editor.AutoIndent == cfg.Editor.AutoIndent &&
		check.Editor.FormatOnSave == cfg.Editor.FormatOnSave &&
		check.Editor.WordWrap == cfg.Editor.WordWrap &&
		check.Editor.ScrollMargin == cfg.Editor.ScrollMargin &&
		check.UI.Theme == cfg.UI.Theme &&
		check.UI.ShowTree == cfg.UI.ShowTree &&
		check.UI.TreeWidth == cfg.UI.TreeWidth
}
