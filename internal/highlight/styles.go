package highlight

import (
	"github.com/alecthomas/chroma/v2"
	"charm.land/lipgloss/v2"
	"teak/internal/ui"
)

// buildStyleMap creates a mapping from chroma token types to lipgloss styles.
// Uses the Nord color palette.
func buildStyleMap(theme ui.Theme) map[chroma.TokenType]lipgloss.Style {
	m := make(map[chroma.TokenType]lipgloss.Style)

	// Keywords
	m[chroma.Keyword] = lipgloss.NewStyle().Foreground(ui.Nord9)
	m[chroma.KeywordConstant] = lipgloss.NewStyle().Foreground(ui.Nord9)
	m[chroma.KeywordDeclaration] = lipgloss.NewStyle().Foreground(ui.Nord9)
	m[chroma.KeywordNamespace] = lipgloss.NewStyle().Foreground(ui.Nord9)
	m[chroma.KeywordReserved] = lipgloss.NewStyle().Foreground(ui.Nord9)
	m[chroma.KeywordType] = lipgloss.NewStyle().Foreground(ui.Nord7)

	// Names
	m[chroma.NameFunction] = lipgloss.NewStyle().Foreground(ui.Nord8)
	m[chroma.NameBuiltin] = lipgloss.NewStyle().Foreground(ui.Nord8)
	m[chroma.NameClass] = lipgloss.NewStyle().Foreground(ui.Nord7)
	m[chroma.NameDecorator] = lipgloss.NewStyle().Foreground(ui.Nord12)
	m[chroma.NameTag] = lipgloss.NewStyle().Foreground(ui.Nord9)
	m[chroma.NameAttribute] = lipgloss.NewStyle().Foreground(ui.Nord8)

	// Literals
	m[chroma.LiteralString] = lipgloss.NewStyle().Foreground(ui.Nord14)
	m[chroma.LiteralStringChar] = lipgloss.NewStyle().Foreground(ui.Nord14)
	m[chroma.LiteralStringBacktick] = lipgloss.NewStyle().Foreground(ui.Nord14)
	m[chroma.LiteralStringDouble] = lipgloss.NewStyle().Foreground(ui.Nord14)
	m[chroma.LiteralStringEscape] = lipgloss.NewStyle().Foreground(ui.Nord13)
	m[chroma.LiteralStringInterpol] = lipgloss.NewStyle().Foreground(ui.Nord14)
	m[chroma.LiteralStringRegex] = lipgloss.NewStyle().Foreground(ui.Nord13)
	m[chroma.LiteralStringSingle] = lipgloss.NewStyle().Foreground(ui.Nord14)

	m[chroma.LiteralNumber] = lipgloss.NewStyle().Foreground(ui.Nord15)
	m[chroma.LiteralNumberFloat] = lipgloss.NewStyle().Foreground(ui.Nord15)
	m[chroma.LiteralNumberHex] = lipgloss.NewStyle().Foreground(ui.Nord15)
	m[chroma.LiteralNumberInteger] = lipgloss.NewStyle().Foreground(ui.Nord15)
	m[chroma.LiteralNumberOct] = lipgloss.NewStyle().Foreground(ui.Nord15)

	// Comments
	m[chroma.Comment] = lipgloss.NewStyle().Foreground(ui.Nord3).Italic(true)
	m[chroma.CommentSingle] = lipgloss.NewStyle().Foreground(ui.Nord3).Italic(true)
	m[chroma.CommentMultiline] = lipgloss.NewStyle().Foreground(ui.Nord3).Italic(true)
	m[chroma.CommentPreproc] = lipgloss.NewStyle().Foreground(ui.Nord10)

	// Operators
	m[chroma.Operator] = lipgloss.NewStyle().Foreground(ui.Nord9)
	m[chroma.OperatorWord] = lipgloss.NewStyle().Foreground(ui.Nord9)

	// Punctuation
	m[chroma.Punctuation] = lipgloss.NewStyle().Foreground(ui.Nord4)

	// Error
	m[chroma.Error] = lipgloss.NewStyle().Foreground(ui.Nord11)

	// Generic
	m[chroma.GenericDeleted] = lipgloss.NewStyle().Foreground(ui.Nord11)
	m[chroma.GenericInserted] = lipgloss.NewStyle().Foreground(ui.Nord14)
	m[chroma.GenericHeading] = lipgloss.NewStyle().Foreground(ui.Nord8).Bold(true)
	m[chroma.GenericSubheading] = lipgloss.NewStyle().Foreground(ui.Nord8)
	m[chroma.GenericEmph] = lipgloss.NewStyle().Italic(true)
	m[chroma.GenericStrong] = lipgloss.NewStyle().Bold(true)

	return m
}
