#!/usr/bin/env python3
"""Minimal LSP server that answers textDocument/completion.

Used by specs/tui_completion_mouse.yml to drive the autocomplete popup in the
interactive editor without an installed language server or network access.
The framing matches lsp-diagnostics-fixture.py.
"""
import json
import sys


def read_message():
    length = None
    while True:
        line = sys.stdin.buffer.readline()
        if not line:
            return None
        line = line.decode("ascii", "replace").strip()
        if not line:
            break
        if line.lower().startswith("content-length:"):
            length = int(line.split(":", 1)[1].strip())
    if length is None:
        return None
    body = sys.stdin.buffer.read(length)
    return json.loads(body)


def send(message):
    body = json.dumps(message, separators=(",", ":")).encode()
    sys.stdout.buffer.write(f"Content-Length: {len(body)}\r\n\r\n".encode())
    sys.stdout.buffer.write(body)
    sys.stdout.buffer.flush()


while True:
    request = read_message()
    if request is None:
        break
    method = request.get("method")
    if method == "initialize":
        send({
            "jsonrpc": "2.0",
            "id": request.get("id"),
            "result": {
                "capabilities": {
                    "positionEncoding": "utf-8",
                    "textDocumentSync": 1,
                    "completionProvider": {"triggerCharacters": []},
                }
            },
        })
    elif method == "textDocument/completion":
        send({
            "jsonrpc": "2.0",
            "id": request.get("id"),
            "result": [
                {"label": "fixturefmt", "insertText": "fixturefmt", "detail": "fixture module"},
                {"label": "fixturefun", "insertText": "fixturefun", "detail": "fixture func"},
            ],
        })
    elif method == "shutdown":
        send({"jsonrpc": "2.0", "id": request.get("id"), "result": None})
    elif method == "exit":
        break
