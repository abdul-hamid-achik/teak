package lsp

// Handler processes LSP server notifications.
// Currently this is handled inline in the Client's readLoop via handleMessage.
// This file provides a place for future expansion of notification handling.

// NotificationHandler can be implemented to receive server notifications.
type NotificationHandler interface {
	HandleDiagnostics(msg DiagnosticsMsg)
}
