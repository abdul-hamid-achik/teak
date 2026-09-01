package app

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"teak/internal/editor/overlays"
	"teak/internal/lsp"
)

// completionItemsPreparedMsg carries only background-converted completion
// items. Request metadata revalidates document/cursor identity, while the
// editor-owned generation rejects Escape, later typing, and closed tabs.
type completionItemsPreparedMsg struct {
	EditorID               uint64
	AutocompleteGeneration uint64
	Metadata               lsp.OverlayRequestMetadata
	Items                  []overlays.AutocompleteItem
	Err                    error
}

func prepareCompletionItemsCmd(ctx context.Context, editorID, generation uint64, metadata lsp.OverlayRequestMetadata, source []lsp.CompletionItem) tea.Cmd {
	return func() tea.Msg {
		items, err := lspCompletionItemsContext(ctx, source)
		return completionItemsPreparedMsg{
			EditorID:               editorID,
			AutocompleteGeneration: generation,
			Metadata:               metadata,
			Items:                  items,
			Err:                    err,
		}
	}
}

func lspCompletionItemsContext(ctx context.Context, source []lsp.CompletionItem) ([]overlays.AutocompleteItem, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	items := make([]overlays.AutocompleteItem, len(source))
	for i, item := range source {
		if i%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		mapped := overlays.AutocompleteItem{
			Label:      item.Label,
			Detail:     item.Detail,
			InsertText: item.InsertText,
			HasEdit:    item.HasEdit,
			Edit: overlays.AutocompleteEdit{
				StartLine: item.Edit.StartLine,
				StartCol:  item.Edit.StartCol,
				EndLine:   item.Edit.EndLine,
				EndCol:    item.Edit.EndCol,
			},
		}
		if len(item.AdditionalEdits) > 0 {
			mapped.AdditionalEdits = make([]overlays.AutocompleteTextEdit, len(item.AdditionalEdits))
			for j, extra := range item.AdditionalEdits {
				mapped.AdditionalEdits[j] = overlays.AutocompleteTextEdit{
					StartLine: extra.StartLine,
					StartCol:  extra.StartCol,
					EndLine:   extra.EndLine,
					EndCol:    extra.EndCol,
					NewText:   extra.NewText,
				}
			}
		}
		items[i] = mapped
	}
	return items, ctx.Err()
}
