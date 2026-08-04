#!/usr/bin/env python3
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
    command = request.get("command")
    if command == "initialize":
        send({
            "seq": 1,
            "type": "response",
            "request_seq": request.get("seq"),
            "command": command,
            "success": True,
            "body": {"supportsConfigurationDoneRequest": True},
        })
    elif command == "disconnect":
        send({
            "seq": 2,
            "type": "response",
            "request_seq": request.get("seq"),
            "command": command,
            "success": True,
        })
        break
