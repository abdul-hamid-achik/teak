#!/usr/bin/env python3
"""Tiny deterministic ACP agent used by the interactive Glyphrun contract."""

import json
import sys


REQUEST_PERMISSION = "--permission" in sys.argv[1:]


def respond(request_id, result):
    print(json.dumps({"jsonrpc": "2.0", "id": request_id, "result": result}), flush=True)


def request(request_id, method, params):
    print(
        json.dumps(
            {"jsonrpc": "2.0", "id": request_id, "method": method, "params": params}
        ),
        flush=True,
    )


def notify(method, params):
    print(json.dumps({"jsonrpc": "2.0", "method": method, "params": params}), flush=True)


def next_message():
    while True:
        line = sys.stdin.readline()
        if not line:
            return None
        if line.strip():
            return json.loads(line)


while True:
    incoming = next_message()
    if incoming is None:
        break
    method = incoming.get("method")
    request_id = incoming.get("id")
    params = incoming.get("params") or {}
    if method == "initialize":
        respond(
            request_id,
            {
                "protocolVersion": 1,
                "agentCapabilities": {"loadSession": False},
                "authMethods": [],
            },
        )
    elif method == "session/new":
        respond(request_id, {"sessionId": "glyphrun-session"})
    elif method == "session/prompt":
        session_id = params.get("sessionId", "glyphrun-session")
        response_text = "GLYPHRUN_AGENT_RESPONSE"
        if REQUEST_PERMISSION:
            permission_id = "glyphrun-permission-request"
            request(
                permission_id,
                "session/request_permission",
                {
                    "sessionId": session_id,
                    "toolCall": {
                        "toolCallId": "glyphrun-tool-call",
                        "title": "Run a safe fixture command",
                        "kind": "execute",
                    },
                    "options": [
                        {
                            "kind": "allow_once",
                            "name": "Allow once",
                            "optionId": "allow-once",
                        },
                        {
                            "kind": "reject_once",
                            "name": "Reject",
                            "optionId": "reject-once",
                        },
                    ],
                },
            )
            while True:
                decision = next_message()
                if decision is None:
                    break
                if decision.get("id") != permission_id:
                    continue
                outcome = decision.get("result", {}).get("outcome", {})
                allowed = outcome.get("outcome") == "selected" and outcome.get("optionId") == "allow-once"
                response_text = "PERMISSION_GRANTED" if allowed else "PERMISSION_NOT_GRANTED"
                break
        notify(
            "session/update",
            {
                "sessionId": session_id,
                "update": {
                    "sessionUpdate": "agent_message_chunk",
                    "content": {"type": "text", "text": response_text},
                },
            },
        )
        respond(request_id, {"stopReason": "end_turn"})
    elif method == "session/set_mode":
        respond(request_id, {})
    elif method == "session/set_model":
        respond(request_id, {})
    elif method == "session/cancel":
        # Cancellation is a notification, so there is no response to send.
        continue
