package editor

// Config holds editor configuration.
type Config struct {
	TabSize       int
	InsertTabs    bool
	AutoIndent    bool
	CommentPrefix string
	WordWrap      bool
	// ScrollMargin keeps the cursor at least this many rows away from the
	// top and bottom viewport edges (vim-style scrolloff).
	ScrollMargin        int
	InsertFinalNewline  bool
	GitGutter           bool
	IndentGuides        bool
	HighlightTrailingWS bool
	RulerColumn         int
}

// DefaultConfig returns the default editor configuration.
func DefaultConfig() Config {
	return Config{
		TabSize:             4,
		InsertTabs:          false,
		AutoIndent:          true,
		ScrollMargin:        2,
		GitGutter:           true,
		IndentGuides:        true,
		HighlightTrailingWS: true,
	}
}
