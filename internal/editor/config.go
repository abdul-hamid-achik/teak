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
	ScrollMargin int
}

// DefaultConfig returns the default editor configuration.
func DefaultConfig() Config {
	return Config{
		TabSize:      4,
		InsertTabs:   false,
		AutoIndent:   true,
		ScrollMargin: 2,
	}
}
