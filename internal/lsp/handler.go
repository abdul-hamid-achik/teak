package lsp

// Note: LSP server notifications are currently handled inline in the Client's
// readLoop via the handleMessage method. This approach is simple and sufficient
// for the current implementation. If more complex notification handling is needed
// in the future, consider implementing a proper handler/dispatcher pattern here.
