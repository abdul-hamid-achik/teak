#!/usr/bin/env python3
import json
import os
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
        marker = os.environ.get("TEAK_LSP_PROBE_MARKER")
        if marker:
            with open(marker, "a", encoding="utf-8") as handle:
                handle.write("probe\n")
        send({
            "jsonrpc": "2.0",
            "id": request.get("id"),
            "result": {
                "capabilities": {
                    "positionEncoding": "utf-8",
                    "textDocumentSync": 1,
                    "documentFormattingProvider": True,
                    "hoverProvider": True,
                    "definitionProvider": True,
                    "referencesProvider": True,
                    "documentSymbolProvider": True,
                }
            },
        })
    elif method == "textDocument/didOpen":
        uri = request["params"]["textDocument"]["uri"]
        send({
            "jsonrpc": "2.0",
            "method": "textDocument/publishDiagnostics",
            "params": {
                "uri": uri,
                "version": 1,
                "diagnostics": [{
                    "severity": 1,
                    "source": "fixture",
                    "message": "fixture diagnostic",
                    "range": {
                        "start": {"line": 0, "character": 1},
                        "end": {"line": 0, "character": 4},
                    },
                }],
            },
        })
    elif method == "textDocument/formatting":
        send({
            "jsonrpc": "2.0",
            "id": request.get("id"),
            "result": [{
                "range": {
                    "start": {"line": 0, "character": 0},
                    "end": {"line": 0, "character": 14},
                },
                "newText": "formatted source",
            }],
        })
    elif method == "textDocument/hover":
        send({
            "jsonrpc": "2.0",
            "id": request.get("id"),
            "result": {"contents": {"kind": "plaintext", "value": "fixture hover"}},
        })
    elif method == "textDocument/definition":
        uri = request["params"]["textDocument"]["uri"]
        send({
            "jsonrpc": "2.0",
            "id": request.get("id"),
            "result": [{
                "uri": uri,
                "range": {
                    "start": {"line": 0, "character": 0},
                    "end": {"line": 0, "character": 14},
                },
            }],
        })
    elif method == "textDocument/references":
        uri = request["params"]["textDocument"]["uri"]
        send({
            "jsonrpc": "2.0",
            "id": request.get("id"),
            "result": [{
                "uri": uri,
                "range": {
                    "start": {"line": 0, "character": 0},
                    "end": {"line": 0, "character": 14},
                },
            }],
        })
    elif method == "textDocument/documentSymbol":
        send({
            "jsonrpc": "2.0",
            "id": request.get("id"),
            "result": [{
                "name": "Fixture",
                "kind": 12,
                "range": {
                    "start": {"line": 0, "character": 0},
                    "end": {"line": 0, "character": 14},
                },
                "selectionRange": {
                    "start": {"line": 0, "character": 0},
                    "end": {"line": 0, "character": 7},
                },
            }],
        })
    elif method == "shutdown":
        send({"jsonrpc": "2.0", "id": request.get("id"), "result": None})
    elif method == "exit":
        break
