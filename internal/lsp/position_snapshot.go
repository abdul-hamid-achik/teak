package lsp

import (
	"fmt"
)

func (c *Client) protocolPositionToInternal(uri string, position Position) (Position, error) {
	encoding := c.negotiatedPositionEncoding()
	snapshot, ok := c.documentSnapshot(uri)
	if !ok {
		// Preserve the historical UTF-8 path for direct clients and virtual
		// documents. UTF-16/32 must never be guessed as byte coordinates and
		// must not trigger server-controlled file I/O in the protocol reader.
		if encoding == positionEncodingUTF8 {
			return position, nil
		}
		return Position{}, fmt.Errorf("no open document snapshot for %s", uri)
	}
	return positionFromProtocolSnapshot(snapshot, encoding, position)
}

func (c *Client) internalPositionToProtocol(uri string, position Position) (Position, error) {
	encoding := c.negotiatedPositionEncoding()
	snapshot, ok := c.documentSnapshot(uri)
	if !ok {
		if encoding == positionEncodingUTF8 {
			return position, nil
		}
		return Position{}, fmt.Errorf("no open document snapshot for %s", uri)
	}
	return positionToProtocolSnapshot(snapshot, encoding, position)
}

func (c *Client) protocolRangeToInternal(uri string, value Range) (Range, error) {
	start, err := c.protocolPositionToInternal(uri, value.Start)
	if err != nil {
		return Range{}, fmt.Errorf("range start: %w", err)
	}
	end, err := c.protocolPositionToInternal(uri, value.End)
	if err != nil {
		return Range{}, fmt.Errorf("range end: %w", err)
	}
	return Range{Start: start, End: end}, nil
}

func (c *Client) internalRangeToProtocol(uri string, value Range) (Range, error) {
	start, err := c.internalPositionToProtocol(uri, value.Start)
	if err != nil {
		return Range{}, fmt.Errorf("range start: %w", err)
	}
	end, err := c.internalPositionToProtocol(uri, value.End)
	if err != nil {
		return Range{}, fmt.Errorf("range end: %w", err)
	}
	return Range{Start: start, End: end}, nil
}

func (c *Client) textEditsFromProtocol(uri string, edits []TextEdit) ([]TextEdit, error) {
	if len(edits) == 0 {
		return nil, nil
	}
	converted := make([]TextEdit, len(edits))
	for index, edit := range edits {
		start, err := c.protocolPositionToInternal(uri, Position{Line: edit.StartLine, Character: edit.StartCol})
		if err != nil {
			return nil, fmt.Errorf("edit %d start: %w", index, err)
		}
		end, err := c.protocolPositionToInternal(uri, Position{Line: edit.EndLine, Character: edit.EndCol})
		if err != nil {
			return nil, fmt.Errorf("edit %d end: %w", index, err)
		}
		converted[index] = TextEdit{
			StartLine: start.Line,
			StartCol:  start.Character,
			EndLine:   end.Line,
			EndCol:    end.Character,
			NewText:   edit.NewText,
		}
	}
	return converted, nil
}

func (c *Client) diagnosticFromProtocol(uri string, diagnostic Diagnostic) (Diagnostic, error) {
	rangeValue, err := c.protocolRangeToInternal(uri, Range{
		Start: Position{Line: diagnostic.Range.Start.Line, Character: diagnostic.Range.Start.Character},
		End:   Position{Line: diagnostic.Range.End.Line, Character: diagnostic.Range.End.Character},
	})
	if err != nil {
		return Diagnostic{}, err
	}
	diagnostic.Range = DiagRange{
		Start: DiagPosition{Line: rangeValue.Start.Line, Character: rangeValue.Start.Character},
		End:   DiagPosition{Line: rangeValue.End.Line, Character: rangeValue.End.Character},
	}
	return diagnostic, nil
}

func (c *Client) diagnosticToProtocol(uri string, diagnostic Diagnostic) (Diagnostic, error) {
	rangeValue, err := c.internalRangeToProtocol(uri, Range{
		Start: Position{Line: diagnostic.Range.Start.Line, Character: diagnostic.Range.Start.Character},
		End:   Position{Line: diagnostic.Range.End.Line, Character: diagnostic.Range.End.Character},
	})
	if err != nil {
		return Diagnostic{}, err
	}
	diagnostic.Range = DiagRange{
		Start: DiagPosition{Line: rangeValue.Start.Line, Character: rangeValue.Start.Character},
		End:   DiagPosition{Line: rangeValue.End.Line, Character: rangeValue.End.Character},
	}
	return diagnostic, nil
}

func (c *Client) diagnosticsToProtocol(uri string, diagnostics []Diagnostic) ([]map[string]any, error) {
	converted := make([]map[string]any, 0, len(diagnostics))
	for index, diagnostic := range diagnostics {
		value, err := c.diagnosticToProtocol(uri, diagnostic)
		if err != nil {
			return nil, fmt.Errorf("diagnostic %d: %w", index, err)
		}
		converted = append(converted, map[string]any{
			"range": map[string]any{
				"start": map[string]any{"line": value.Range.Start.Line, "character": value.Range.Start.Character},
				"end":   map[string]any{"line": value.Range.End.Line, "character": value.Range.End.Character},
			},
			"severity": value.Severity,
			"message":  value.Message,
			"source":   value.Source,
		})
	}
	return converted, nil
}

func (c *Client) workspaceEditFromProtocol(edit WorkspaceEdit) (WorkspaceEdit, error) {
	converted := WorkspaceEdit{}
	if len(edit.Changes) > 0 {
		converted.Changes = make(map[string][]TextEdit, len(edit.Changes))
		for uri, edits := range edit.Changes {
			value, err := c.textEditsFromProtocol(uri, edits)
			if err != nil {
				return WorkspaceEdit{}, fmt.Errorf("workspace changes for %s: %w", uri, err)
			}
			converted.Changes[uri] = value
		}
	}
	if len(edit.DocumentChanges) > 0 {
		converted.DocumentChanges = make([]WorkspaceDocumentChange, 0, len(edit.DocumentChanges))
		for _, change := range edit.DocumentChanges {
			if change.FileOperation != nil {
				operation := *change.FileOperation
				converted.DocumentChanges = append(converted.DocumentChanges, WorkspaceDocumentChange{FileOperation: &operation})
				continue
			}
			value, err := c.textEditsFromProtocol(change.URI, change.Edits)
			if err != nil {
				return WorkspaceEdit{}, fmt.Errorf("workspace document change for %s: %w", change.URI, err)
			}
			converted.DocumentChanges = append(converted.DocumentChanges, WorkspaceDocumentChange{
				URI:     change.URI,
				Version: change.Version,
				Edits:   value,
			})
		}
	}
	return converted, nil
}

func (c *Client) codeActionsFromProtocol(uri string, actions []CodeAction) ([]CodeAction, error) {
	converted := make([]CodeAction, len(actions))
	for index, action := range actions {
		diagnostics := make([]Diagnostic, 0, len(action.Diagnostics))
		for diagnosticIndex, diagnostic := range action.Diagnostics {
			value, err := c.diagnosticFromProtocol(uri, diagnostic)
			if err != nil {
				return nil, fmt.Errorf("code action %d diagnostic %d: %w", index, diagnosticIndex, err)
			}
			diagnostics = append(diagnostics, value)
		}
		if action.Edit != nil {
			edit, err := c.workspaceEditFromProtocol(*action.Edit)
			if err != nil {
				return nil, fmt.Errorf("code action %d workspace edit: %w", index, err)
			}
			action.Edit = &edit
		}
		action.Diagnostics = diagnostics
		converted[index] = action
	}
	return converted, nil
}

func (c *Client) locationFromProtocol(location Location) (Location, error) {
	if _, ok := c.documentSnapshot(location.URI); !ok && c.negotiatedPositionEncoding() != positionEncodingUTF8 {
		// Locations may point at a file the user has not opened. Do not read a
		// server-controlled URI on the JSON-RPC reader; retain the negotiated
		// coordinates for conversion after the app's existing async file load.
		location.ProtocolEncoding = string(c.negotiatedPositionEncoding())
		return location, nil
	}
	start, err := c.protocolPositionToInternal(location.URI, Position{Line: location.StartLine, Character: location.StartCol})
	if err != nil {
		return Location{}, fmt.Errorf("location start: %w", err)
	}
	end, err := c.protocolPositionToInternal(location.URI, Position{Line: location.EndLine, Character: location.EndCol})
	if err != nil {
		return Location{}, fmt.Errorf("location end: %w", err)
	}
	location.StartLine = start.Line
	location.StartCol = start.Character
	location.EndLine = end.Line
	location.EndCol = end.Character
	return location, nil
}

func (c *Client) locationsFromProtocol(locations []Location) ([]Location, error) {
	converted := make([]Location, 0, len(locations))
	for index, location := range locations {
		value, err := c.locationFromProtocol(location)
		if err != nil {
			return nil, fmt.Errorf("location %d: %w", index, err)
		}
		converted = append(converted, value)
	}
	return converted, nil
}

func (c *Client) foldingRangesFromProtocol(uri string, ranges []FoldingRange) ([]FoldingRange, error) {
	converted := make([]FoldingRange, len(ranges))
	for index, value := range ranges {
		start, err := c.protocolPositionToInternal(uri, Position{Line: value.StartLine, Character: value.StartCharacter})
		if err != nil {
			return nil, fmt.Errorf("folding range %d start: %w", index, err)
		}
		end, err := c.protocolPositionToInternal(uri, Position{Line: value.EndLine, Character: value.EndCharacter})
		if err != nil {
			return nil, fmt.Errorf("folding range %d end: %w", index, err)
		}
		value.StartLine = start.Line
		value.StartCharacter = start.Character
		value.EndLine = end.Line
		value.EndCharacter = end.Character
		converted[index] = value
	}
	return converted, nil
}

func (c *Client) documentSymbolsFromProtocol(uri string, symbols []DocumentSymbol) ([]DocumentSymbol, error) {
	converted := make([]DocumentSymbol, len(symbols))
	for index, symbol := range symbols {
		rangeValue, err := c.protocolRangeToInternal(uri, symbol.Range)
		if err != nil {
			return nil, fmt.Errorf("symbol %d range: %w", index, err)
		}
		selection, err := c.protocolRangeToInternal(uri, symbol.SelectionRange)
		if err != nil {
			return nil, fmt.Errorf("symbol %d selection range: %w", index, err)
		}
		children, err := c.documentSymbolsFromProtocol(uri, symbol.Children)
		if err != nil {
			return nil, err
		}
		symbol.Range = rangeValue
		symbol.SelectionRange = selection
		symbol.Children = children
		converted[index] = symbol
	}
	return converted, nil
}
