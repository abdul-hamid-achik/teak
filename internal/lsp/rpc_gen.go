// LSP RPC method implementations. The Context variants accept caller
// cancellation while retaining a method-specific maximum duration. The
// original methods remain compatibility wrappers for embedded callers.

package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

func rpcRequestContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, timeout)
}

// Completion requests completion items at the given position.
func (c *Client) Completion(uri string, line, character int) ([]CompletionItem, error) {
	return c.CompletionContext(context.Background(), uri, line, character)
}

// CompletionContext requests completion items and honors caller cancellation.
func (c *Client) CompletionContext(ctx context.Context, uri string, line, character int) ([]CompletionItem, error) {
	ctx, cancel := rpcRequestContext(ctx, 10*time.Second)
	defer cancel()
	position, err := c.internalPositionToProtocol(uri, Position{Line: line, Character: character})
	if err != nil {
		return nil, fmt.Errorf("completion position: %w", err)
	}

	result, err := c.call(ctx, "textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": position.Line, "character": position.Character},
	})
	if err != nil {
		return nil, err
	}

	var list struct {
		Items []rawCompletionItem `json:"items"`
	}
	if err := json.Unmarshal(result, &list); err == nil && len(list.Items) > 0 {
		return c.completionItems(uri, list.Items), nil
	}
	var plainItems []rawCompletionItem
	if err := json.Unmarshal(result, &plainItems); err == nil {
		return c.completionItems(uri, plainItems), nil
	}
	return nil, nil
}

// rawCompletionItem mirrors the wire shape of a completion item. Servers may
// describe the insertion as plain text, as a textEdit with an explicit range,
// or as an insertReplaceEdit carrying both an insert and a replace range.
type rawCompletionItem struct {
	Label      string `json:"label"`
	Detail     string `json:"detail"`
	InsertText string `json:"insertText"`
	Kind       int    `json:"kind"`
	TextEdit   *struct {
		Range   *Range `json:"range"`
		Insert  *Range `json:"insert"`
		Replace *Range `json:"replace"`
		NewText string `json:"newText"`
	} `json:"textEdit"`
	AdditionalTextEdits []struct {
		Range   *Range `json:"range"`
		NewText string `json:"newText"`
	} `json:"additionalTextEdits"`
}

func (c *Client) completionItems(uri string, raw []rawCompletionItem) []CompletionItem {
	items := make([]CompletionItem, 0, len(raw))
	for _, item := range raw {
		converted := CompletionItem{
			Label:  item.Label,
			Detail: item.Detail,
			Kind:   item.Kind,
		}

		// insertText is the fallback text, and label the fallback for that, but
		// a textEdit always takes precedence because only it knows how much of
		// what the user typed the replacement is meant to cover.
		converted.InsertText = item.InsertText
		if converted.InsertText == "" {
			converted.InsertText = item.Label
		}

		if edit := item.TextEdit; edit != nil {
			// Prefer replace over insert: accepting a completion should
			// overwrite the identifier being typed rather than splice into it.
			protocolRange := edit.Range
			if protocolRange == nil {
				protocolRange = edit.Replace
			}
			if protocolRange == nil {
				protocolRange = edit.Insert
			}
			if protocolRange != nil {
				if internal, err := c.protocolRangeToInternal(uri, *protocolRange); err == nil {
					converted.Edit = CompletionEdit{
						StartLine: internal.Start.Line,
						StartCol:  internal.Start.Character,
						EndLine:   internal.End.Line,
						EndCol:    internal.End.Character,
						NewText:   edit.NewText,
					}
					converted.HasEdit = true
					converted.InsertText = edit.NewText
				}
			}
		}

		for _, extra := range item.AdditionalTextEdits {
			if extra.Range == nil {
				continue
			}
			internal, err := c.protocolRangeToInternal(uri, *extra.Range)
			if err != nil {
				continue
			}
			converted.AdditionalEdits = append(converted.AdditionalEdits, CompletionEdit{
				StartLine: internal.Start.Line,
				StartCol:  internal.Start.Character,
				EndLine:   internal.End.Line,
				EndCol:    internal.End.Character,
				NewText:   extra.NewText,
			})
		}

		items = append(items, converted)
	}
	return items
}

// Hover requests hover info at the given position.
func (c *Client) Hover(uri string, line, character int) (*HoverResult, error) {
	return c.HoverContext(context.Background(), uri, line, character)
}

// HoverContext requests hover info and honors caller cancellation.
func (c *Client) HoverContext(ctx context.Context, uri string, line, character int) (*HoverResult, error) {
	ctx, cancel := rpcRequestContext(ctx, 5*time.Second)
	defer cancel()
	position, err := c.internalPositionToProtocol(uri, Position{Line: line, Character: character})
	if err != nil {
		return nil, fmt.Errorf("hover position: %w", err)
	}

	result, err := c.call(ctx, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": position.Line, "character": position.Character},
	})
	if err != nil {
		return nil, err
	}

	if string(result) == "null" {
		return nil, nil
	}

	var hover struct {
		Contents any `json:"contents"`
	}
	if err := json.Unmarshal(result, &hover); err != nil {
		return nil, err
	}

	content := extractHoverContent(hover.Contents)
	if content == "" {
		return nil, nil
	}

	return &HoverResult{Content: content}, nil
}

// Definition requests go-to-definition at the given position.
func (c *Client) Definition(uri string, line, character int) ([]Location, error) {
	return c.DefinitionContext(context.Background(), uri, line, character)
}

// DefinitionContext requests go-to-definition and honors caller cancellation.
func (c *Client) DefinitionContext(ctx context.Context, uri string, line, character int) ([]Location, error) {
	ctx, cancel := rpcRequestContext(ctx, 5*time.Second)
	defer cancel()
	position, err := c.internalPositionToProtocol(uri, Position{Line: line, Character: character})
	if err != nil {
		return nil, fmt.Errorf("definition position: %w", err)
	}

	result, err := c.call(ctx, "textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": position.Line, "character": position.Character},
	})
	if err != nil {
		return nil, err
	}

	if string(result) == "null" {
		return nil, nil
	}

	var locations []Location
	var locs []struct {
		URI   string `json:"uri"`
		Range struct {
			Start struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"start"`
			End struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"end"`
		} `json:"range"`
	}
	if err := json.Unmarshal(result, &locs); err == nil {
		for _, loc := range locs {
			locations = append(locations, Location{
				URI:       loc.URI,
				StartLine: loc.Range.Start.Line,
				StartCol:  loc.Range.Start.Character,
				EndLine:   loc.Range.End.Line,
				EndCol:    loc.Range.End.Character,
			})
		}
		return c.locationsFromProtocol(locations)
	}
	var single struct {
		URI   string `json:"uri"`
		Range struct {
			Start struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"start"`
			End struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"end"`
		} `json:"range"`
	}
	if err := json.Unmarshal(result, &single); err == nil && single.URI != "" {
		locations = append(locations, Location{
			URI:       single.URI,
			StartLine: single.Range.Start.Line,
			StartCol:  single.Range.Start.Character,
			EndLine:   single.Range.End.Line,
			EndCol:    single.Range.End.Character,
		})
	}
	return c.locationsFromProtocol(locations)
}

// References requests find-references at the given position.
func (c *Client) References(uri string, line, character int) ([]Location, error) {
	return c.ReferencesContext(context.Background(), uri, line, character)
}

// ReferencesContext requests references and honors caller cancellation.
func (c *Client) ReferencesContext(ctx context.Context, uri string, line, character int) ([]Location, error) {
	ctx, cancel := rpcRequestContext(ctx, 5*time.Second)
	defer cancel()
	position, err := c.internalPositionToProtocol(uri, Position{Line: line, Character: character})
	if err != nil {
		return nil, fmt.Errorf("references position: %w", err)
	}

	result, err := c.call(ctx, "textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": position.Line, "character": position.Character},
		"context":      map[string]any{"includeDeclaration": true},
	})
	if err != nil {
		return nil, err
	}

	if string(result) == "null" {
		return nil, nil
	}

	var locations []Location
	var locs []struct {
		URI   string `json:"uri"`
		Range struct {
			Start struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"start"`
			End struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"end"`
		} `json:"range"`
	}
	if err := json.Unmarshal(result, &locs); err == nil {
		for _, loc := range locs {
			locations = append(locations, Location{
				URI:       loc.URI,
				StartLine: loc.Range.Start.Line,
				StartCol:  loc.Range.Start.Character,
				EndLine:   loc.Range.End.Line,
				EndCol:    loc.Range.End.Character,
			})
		}
	}
	return c.locationsFromProtocol(locations)
}

// Rename requests rename at the given position.
func (c *Client) Rename(uri string, line, character int, newName string) (WorkspaceEdit, error) {
	return c.RenameContext(context.Background(), uri, line, character, newName)
}

// RenameContext requests rename and honors caller cancellation.
func (c *Client) RenameContext(ctx context.Context, uri string, line, character int, newName string) (WorkspaceEdit, error) {
	ctx, cancel := rpcRequestContext(ctx, 5*time.Second)
	defer cancel()
	position, err := c.internalPositionToProtocol(uri, Position{Line: line, Character: character})
	if err != nil {
		return WorkspaceEdit{}, fmt.Errorf("rename position: %w", err)
	}

	result, err := c.call(ctx, "textDocument/rename", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": position.Line, "character": position.Character},
		"newName":      newName,
	})
	if err != nil {
		return WorkspaceEdit{}, err
	}

	if string(result) == "null" {
		return WorkspaceEdit{}, nil
	}

	var edit WorkspaceEdit
	if err := json.Unmarshal(result, &edit); err != nil {
		return WorkspaceEdit{}, err
	}
	converted, err := c.workspaceEditFromProtocol(edit)
	if err != nil {
		return WorkspaceEdit{}, err
	}
	return converted, nil
}

// SignatureHelp requests signature help at the given position.
func (c *Client) SignatureHelp(uri string, line, character int) (*SignatureHelp, error) {
	return c.SignatureHelpContext(context.Background(), uri, line, character)
}

// SignatureHelpContext requests signature help and honors caller cancellation.
func (c *Client) SignatureHelpContext(ctx context.Context, uri string, line, character int) (*SignatureHelp, error) {
	ctx, cancel := rpcRequestContext(ctx, 3*time.Second)
	defer cancel()
	position, err := c.internalPositionToProtocol(uri, Position{Line: line, Character: character})
	if err != nil {
		return nil, fmt.Errorf("signature help position: %w", err)
	}

	result, err := c.call(ctx, "textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": position.Line, "character": position.Character},
	})
	if err != nil {
		return nil, err
	}

	if string(result) == "null" {
		return nil, nil
	}

	var help SignatureHelp
	if err := json.Unmarshal(result, &help); err != nil {
		return nil, err
	}
	return &help, nil
}

// Formatting requests formatting for a document.
func (c *Client) Formatting(uri string, options FormattingOptions) ([]TextEdit, error) {
	return c.FormattingContext(context.Background(), uri, options)
}

// FormattingContext requests formatting and honors caller cancellation.
func (c *Client) FormattingContext(ctx context.Context, uri string, options FormattingOptions) ([]TextEdit, error) {
	if !c.SupportsFormatting() {
		return nil, nil
	}

	ctx, cancel := rpcRequestContext(ctx, 15*time.Second)
	defer cancel()

	result, err := c.call(ctx, "textDocument/formatting", formattingRequestParams(uri, options))
	if err != nil {
		return nil, err
	}

	if string(result) == "null" {
		return nil, nil
	}

	var edits []struct {
		Range struct {
			Start struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"start"`
			End struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"end"`
		} `json:"range"`
		NewText string `json:"newText"`
	}
	if err := json.Unmarshal(result, &edits); err != nil {
		return nil, err
	}
	var textEdits []TextEdit
	for _, edit := range edits {
		textEdits = append(textEdits, TextEdit{
			StartLine: edit.Range.Start.Line,
			StartCol:  edit.Range.Start.Character,
			EndLine:   edit.Range.End.Line,
			EndCol:    edit.Range.End.Character,
			NewText:   edit.NewText,
		})
	}
	converted, err := c.textEditsFromProtocol(uri, textEdits)
	if err != nil {
		return nil, err
	}
	return converted, nil
}

// FoldingRange requests folding ranges for a document.
func (c *Client) FoldingRange(uri string) ([]FoldingRange, error) {
	return c.FoldingRangeContext(context.Background(), uri)
}

// FoldingRangeContext requests folding ranges and honors caller cancellation.
func (c *Client) FoldingRangeContext(ctx context.Context, uri string) ([]FoldingRange, error) {
	ctx, cancel := rpcRequestContext(ctx, 3*time.Second)
	defer cancel()

	result, err := c.call(ctx, "textDocument/foldingRange", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	if err != nil {
		return nil, err
	}

	if string(result) == "null" {
		return nil, nil
	}

	var ranges []FoldingRange
	if err := json.Unmarshal(result, &ranges); err != nil {
		return nil, err
	}
	converted, err := c.foldingRangesFromProtocol(uri, ranges)
	if err != nil {
		return nil, err
	}
	return converted, nil
}

// CodeAction requests code actions for the given range.
func (c *Client) CodeAction(uri string, startLine, startCol, endLine, endCol int, diagnostics []Diagnostic) ([]CodeAction, error) {
	return c.CodeActionContext(context.Background(), uri, startLine, startCol, endLine, endCol, diagnostics)
}

// CodeActionContext requests code actions and honors caller cancellation.
func (c *Client) CodeActionContext(ctx context.Context, uri string, startLine, startCol, endLine, endCol int, diagnostics []Diagnostic) ([]CodeAction, error) {
	ctx, cancel := rpcRequestContext(ctx, 5*time.Second)
	defer cancel()
	rangeValue, err := c.internalRangeToProtocol(uri, Range{
		Start: Position{Line: startLine, Character: startCol},
		End:   Position{Line: endLine, Character: endCol},
	})
	if err != nil {
		return nil, fmt.Errorf("code action range: %w", err)
	}
	protocolDiagnostics, err := c.diagnosticsToProtocol(uri, diagnostics)
	if err != nil {
		return nil, fmt.Errorf("code action diagnostics: %w", err)
	}

	result, err := c.call(ctx, "textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": map[string]any{"line": rangeValue.Start.Line, "character": rangeValue.Start.Character},
			"end":   map[string]any{"line": rangeValue.End.Line, "character": rangeValue.End.Character},
		},
		"context": map[string]any{"diagnostics": protocolDiagnostics},
	})
	if err != nil {
		return nil, err
	}

	if string(result) == "null" {
		return nil, nil
	}

	var actions []CodeAction
	if err := json.Unmarshal(result, &actions); err != nil {
		return nil, err
	}
	converted, err := c.codeActionsFromProtocol(uri, actions)
	if err != nil {
		return nil, err
	}
	return converted, nil
}

// DocumentSymbol requests document symbols for a document.
func (c *Client) DocumentSymbol(uri string) ([]DocumentSymbol, error) {
	return c.DocumentSymbolContext(context.Background(), uri)
}

// DocumentSymbolContext requests document symbols and honors caller cancellation.
func (c *Client) DocumentSymbolContext(ctx context.Context, uri string) ([]DocumentSymbol, error) {
	ctx, cancel := rpcRequestContext(ctx, 5*time.Second)
	defer cancel()

	result, err := c.call(ctx, "textDocument/documentSymbol", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	if err != nil {
		return nil, err
	}

	if string(result) == "null" {
		return nil, nil
	}

	var wireSymbols []documentSymbolWire
	if err := json.Unmarshal(result, &wireSymbols); err != nil {
		return nil, err
	}
	symbols := make([]DocumentSymbol, len(wireSymbols))
	for index, symbol := range wireSymbols {
		symbols[index] = symbol.documentSymbol()
	}
	converted, err := c.documentSymbolsFromProtocol(uri, symbols)
	if err != nil {
		return nil, err
	}
	return converted, nil
}

// documentSymbolWire accepts both result shapes allowed by the LSP method:
// hierarchical DocumentSymbol values and flat SymbolInformation values whose
// range is nested under location.
type documentSymbolWire struct {
	Name           string               `json:"name"`
	Detail         string               `json:"detail,omitempty"`
	Kind           int                  `json:"kind"`
	Range          Range                `json:"range"`
	SelectionRange Range                `json:"selectionRange"`
	Children       []documentSymbolWire `json:"children,omitempty"`
	ContainerName  string               `json:"containerName,omitempty"`
	Location       *struct {
		URI   string `json:"uri"`
		Range Range  `json:"range"`
	} `json:"location,omitempty"`
}

func (symbol documentSymbolWire) documentSymbol() DocumentSymbol {
	rangeValue := symbol.Range
	selection := symbol.SelectionRange
	detail := symbol.Detail
	if symbol.Location != nil {
		rangeValue = symbol.Location.Range
		selection = rangeValue
		if detail == "" {
			detail = symbol.ContainerName
		}
	}
	children := make([]DocumentSymbol, len(symbol.Children))
	for index, child := range symbol.Children {
		children[index] = child.documentSymbol()
	}
	return DocumentSymbol{
		Name:           symbol.Name,
		Detail:         detail,
		Kind:           symbol.Kind,
		Range:          rangeValue,
		SelectionRange: selection,
		Children:       children,
	}
}
