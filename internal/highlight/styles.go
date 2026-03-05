package highlight

import (
	"github.com/alecthomas/chroma/v2"
	"charm.land/lipgloss/v2"
	"teak/internal/ui"
)

// buildStyleMap creates a mapping from chroma token types to lipgloss styles.
// Uses the theme's semantic syntax colors so highlighting adapts to the active theme.
func buildStyleMap(theme ui.Theme) map[chroma.TokenType]lipgloss.Style {
	m := make(map[chroma.TokenType]lipgloss.Style)

	// Keywords
	m[chroma.Keyword] = lipgloss.NewStyle().Foreground(theme.SyntaxKeyword)
	m[chroma.KeywordConstant] = lipgloss.NewStyle().Foreground(theme.SyntaxKeyword)
	m[chroma.KeywordDeclaration] = lipgloss.NewStyle().Foreground(theme.SyntaxKeyword)
	m[chroma.KeywordNamespace] = lipgloss.NewStyle().Foreground(theme.SyntaxKeyword)
	m[chroma.KeywordReserved] = lipgloss.NewStyle().Foreground(theme.SyntaxKeyword)
	m[chroma.KeywordType] = lipgloss.NewStyle().Foreground(theme.SyntaxType)

	// Names
	m[chroma.NameFunction] = lipgloss.NewStyle().Foreground(theme.SyntaxFunction)
	m[chroma.NameBuiltin] = lipgloss.NewStyle().Foreground(theme.SyntaxFunction)
	m[chroma.NameClass] = lipgloss.NewStyle().Foreground(theme.SyntaxType)
	m[chroma.NameDecorator] = lipgloss.NewStyle().Foreground(theme.SyntaxAttribute)
	m[chroma.NameTag] = lipgloss.NewStyle().Foreground(theme.SyntaxTag)
	m[chroma.NameAttribute] = lipgloss.NewStyle().Foreground(theme.SyntaxAttribute)

	// Literals
	m[chroma.LiteralString] = lipgloss.NewStyle().Foreground(theme.SyntaxString)
	m[chroma.LiteralStringChar] = lipgloss.NewStyle().Foreground(theme.SyntaxString)
	m[chroma.LiteralStringBacktick] = lipgloss.NewStyle().Foreground(theme.SyntaxString)
	m[chroma.LiteralStringDouble] = lipgloss.NewStyle().Foreground(theme.SyntaxString)
	m[chroma.LiteralStringEscape] = lipgloss.NewStyle().Foreground(theme.SyntaxNumber)
	m[chroma.LiteralStringInterpol] = lipgloss.NewStyle().Foreground(theme.SyntaxString)
	m[chroma.LiteralStringRegex] = lipgloss.NewStyle().Foreground(theme.SyntaxNumber)
	m[chroma.LiteralStringSingle] = lipgloss.NewStyle().Foreground(theme.SyntaxString)

	m[chroma.LiteralNumber] = lipgloss.NewStyle().Foreground(theme.SyntaxNumber)
	m[chroma.LiteralNumberFloat] = lipgloss.NewStyle().Foreground(theme.SyntaxNumber)
	m[chroma.LiteralNumberHex] = lipgloss.NewStyle().Foreground(theme.SyntaxNumber)
	m[chroma.LiteralNumberInteger] = lipgloss.NewStyle().Foreground(theme.SyntaxNumber)
	m[chroma.LiteralNumberOct] = lipgloss.NewStyle().Foreground(theme.SyntaxNumber)

	// Comments
	m[chroma.Comment] = lipgloss.NewStyle().Foreground(theme.SyntaxComment).Italic(true)
	m[chroma.CommentSingle] = lipgloss.NewStyle().Foreground(theme.SyntaxComment).Italic(true)
	m[chroma.CommentMultiline] = lipgloss.NewStyle().Foreground(theme.SyntaxComment).Italic(true)
	m[chroma.CommentPreproc] = lipgloss.NewStyle().Foreground(theme.SyntaxKeyword)

	// Operators
	m[chroma.Operator] = lipgloss.NewStyle().Foreground(theme.SyntaxOperator)
	m[chroma.OperatorWord] = lipgloss.NewStyle().Foreground(theme.SyntaxOperator)

	// Punctuation — use the editor foreground (neutral)
	m[chroma.Punctuation] = theme.Editor

	// Error
	m[chroma.Error] = lipgloss.NewStyle().Foreground(ui.Nord11)

	// Generic
	m[chroma.GenericDeleted] = lipgloss.NewStyle().Foreground(ui.Nord11)
	m[chroma.GenericInserted] = lipgloss.NewStyle().Foreground(theme.SyntaxString)
	m[chroma.GenericHeading] = lipgloss.NewStyle().Foreground(theme.SyntaxFunction).Bold(true)
	m[chroma.GenericSubheading] = lipgloss.NewStyle().Foreground(theme.SyntaxFunction)
	m[chroma.GenericEmph] = lipgloss.NewStyle().Italic(true)
	m[chroma.GenericStrong] = lipgloss.NewStyle().Bold(true)

	return m
}
