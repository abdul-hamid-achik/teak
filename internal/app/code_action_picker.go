package app

import (
	"fmt"

	"teak/internal/lsp"
	"teak/internal/overlay"
)

func lspCodeActionsToPickerItems(actions []lsp.CodeAction, metadata lsp.DocumentRequestMetadata) []overlay.PickerItem {
	items := make([]overlay.PickerItem, 0, len(actions))
	for _, action := range actions {
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
	return items
}

func joinCodeActionDescription(kind, behavior string) string {
	if kind == "" {
		return behavior
	}
	return fmt.Sprintf("%s · %s", kind, behavior)
}
