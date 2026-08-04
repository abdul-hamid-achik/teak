package overlays

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"teak/internal/ui"
)

// SignatureHelp manages the signature help popup state.
type SignatureHelp struct {
	Help      *SignatureData
	Visible   bool
	theme     ui.Theme
	maxWidth  int
	maxHeight int
}

// SignatureData holds signature information.
type SignatureData struct {
	Signatures      []SignatureInfo
	ActiveSignature int
	ActiveParameter int
}

// SignatureInfo holds information about a single signature.
type SignatureInfo struct {
	Label         string
	Documentation string
	Parameters    []ParameterInfo
}

// ParameterInfo holds information about a parameter.
type ParameterInfo struct {
	Label         string
	Documentation string
}

// NewSignatureHelp creates a new signature help popup.
func NewSignatureHelp(theme ui.Theme) SignatureHelp {
	return SignatureHelp{
		theme:     theme,
		maxWidth:  70,
		maxHeight: 8,
	}
}

// Show displays signature help.
func (s *SignatureHelp) Show(help *SignatureData) {
	s.Help = help
	s.Visible = help != nil && len(help.Signatures) > 0
}

// Hide dismisses the signature help popup.
func (s *SignatureHelp) Hide() {
	s.Visible = false
	s.Help = nil
}

// View renders the signature help popup.
func (s SignatureHelp) View() string {
	if !s.Visible || s.Help == nil || len(s.Help.Signatures) == 0 {
		return ""
	}

	activeIdx := min(max(0, s.Help.ActiveSignature), len(s.Help.Signatures)-1)
	activeSig := s.Help.Signatures[activeIdx]
	label := activeSig.Label

	// Highlight active parameter
	if s.Help.ActiveParameter >= 0 && s.Help.ActiveParameter < len(activeSig.Parameters) {
		param := activeSig.Parameters[s.Help.ActiveParameter]
		// Simple highlighting: wrap parameter in brackets
		label = strings.Replace(label, param.Label, "["+param.Label+"]", 1)
	}

	// Build content
	var lines []string
	lines = append(lines, truncateCells(label, s.maxWidth))

	// Add documentation if available
	if activeSig.Documentation != "" {
		docLines := strings.Split(activeSig.Documentation, "\n")
		if len(docLines) > 2 {
			docLines = docLines[:2]
		}
		for _, line := range docLines {
			lines = append(lines, truncateCells(line, s.maxWidth))
		}
	}

	// Limit total lines
	if len(lines) > s.maxHeight {
		lines = lines[:s.maxHeight]
	}

	content := strings.Join(lines, "\n")
	return s.theme.HoverBox.Render(content)
}

func truncateCells(value string, width int) string {
	if ansi.StringWidth(value) <= width {
		return value
	}
	if width <= 3 {
		return ansi.Truncate(value, max(0, width), "")
	}
	return ansi.Truncate(value, width-3, "") + "..."
}

// UpdateActiveParameter updates the active parameter index.
func (s *SignatureHelp) UpdateActiveParameter(idx int) {
	if s.Help == nil {
		return
	}
	if idx < 0 {
		idx = 0
	}
	s.Help.ActiveParameter = idx
}
