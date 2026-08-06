package app

import (
	"context"
	"fmt"

	"teak/internal/lsp"
	"teak/internal/overlay"
)

func lspCodeActionsToPickerItemsContext(ctx context.Context, actions []lsp.CodeAction, metadata lsp.DocumentRequestMetadata) ([]overlay.PickerItem, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	items := make([]overlay.PickerItem, 0, len(actions))
	for i, action := range actions {
		if i%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		description := action.Kind
		switch {
		case action.Edit != nil && action.Command != nil:
			description = joinCodeActionDescription(description, "edit + server command")
		case action.Edit != nil:
			description = joinCodeActionDescription(description, "workspace edit")
		case action.Command != nil:
			description = joinCodeActionDescription(description, "server command")
		default:
			description = joinCodeActionDescription(description, "no executable change")
		}
		items = append(items, overlay.PickerItem{
			Label:       action.Title,
			Description: description,
			Value: lspCodeActionPickerMsg{
				Action:   action,
				Metadata: metadata,
			},
		})
	}
	return items, ctx.Err()
}

func joinCodeActionDescription(kind, behavior string) string {
	if kind == "" {
		return behavior
	}
	return fmt.Sprintf("%s · %s", kind, behavior)
}
