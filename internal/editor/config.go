package editor

// Config holds editor configuration.
type Config struct {
	TabSize       int
	InsertTabs    bool
	AutoIndent    bool
	CommentPrefix string
}

// DefaultConfig returns the default editor configuration.
func DefaultConfig() Config {
	return Config{
		TabSize:    4,
		InsertTabs: false,
		AutoIndent: true,
	}
}
