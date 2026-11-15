"""
title: Jett (Hermes Agent)
description: Chat with the Hermes agent and show its tool calls as live status lines.
version: 0.1.0
license: MIT

Open WebUI Pipe function. Install it in Open WebUI: Admin Panel -> Functions -> "+"
(paste this file), then enable it. It appears as its own model, "Jett (Hermes Agent)".

Why: Hermes's OpenAI-compatible API server reports tool activity as custom SSE frames
(`event: hermes.tool.progress`), which Open WebUI's plain OpenAI connection ignores.
This pipe forwards the chat to Hermes, turns those frames into Open WebUI status
events, and streams the answer text through unchanged. Status events are UI-only,
so tool chatter is never sent back to Hermes as conversation history.

Kept in git at namespaces/agent/open-webui-functions/hermes_pipe.py; Open WebUI stores
functions in its database, so re-paste after edits.
"""

import json
import os
from typing import AsyncGenerator, Awaitable, Callable, Optional

import aiohttp
from pydantic import BaseModel, Field


def _env_key() -> str:
    # The open-webui pod already carries the Hermes key as the first entry of
    # OPENAI_API_KEYS (hermes-api-key Secret); prefer that over storing it in a valve.
    return (os.environ.get("OPENAI_API_KEYS", "").split(";")[0] or "").strip()


class Pipe:
    class Valves(BaseModel):
        HERMES_BASE_URL: str = Field(
            default="http://hermes.agent.svc.cluster.local:8642/v1",
            description="Hermes API server base URL (OpenAI-compatible).",
        )
        HERMES_API_KEY: str = Field(
            default="",
            description="Leave empty to use the key from the pod env (OPENAI_API_KEYS).",
        )
        HERMES_MODEL: str = Field(default="jett", description="Hermes API_SERVER_MODEL_NAME.")
        SHOW_TOOL_STATUS: bool = Field(default=True, description="Show tool calls as status lines.")
        TIMEOUT_SECONDS: int = Field(default=1800, description="Total time allowed per agent turn.")

    def __init__(self):
        self.valves = self.Valves()

    def pipes(self):
        return [{"id": "jett", "name": "Jett (Hermes Agent)"}]

    async def _status(self, emit, description: str, done: bool, hidden: bool = False):
        if emit and self.valves.SHOW_TOOL_STATUS:
            await emit({"type": "status", "data": {"description": description, "done": done, "hidden": hidden}})

    async def pipe(
        self,
        body: dict,
        __user__: Optional[dict] = None,
        __event_emitter__: Optional[Callable[[dict], Awaitable[None]]] = None,
    ) -> AsyncGenerator[str, None]:
        key = self.valves.HERMES_API_KEY or _env_key()
        payload = {
            "model": self.valves.HERMES_MODEL,
            "messages": body.get("messages", []),
            "stream": True,
        }
        for opt in ("temperature", "max_tokens", "top_p"):
            if opt in body:
                payload[opt] = body[opt]

        headers = {"Content-Type": "application/json", "Authorization": f"Bearer {key}"}
        url = self.valves.HERMES_BASE_URL.rstrip("/") + "/chat/completions"
        running: dict[str, str] = {}  # toolCallId -> label shown while running

        await self._status(__event_emitter__, "Jett is thinking…", False)
        timeout = aiohttp.ClientTimeout(total=self.valves.TIMEOUT_SECONDS, sock_read=None)
        try:
            async with aiohttp.ClientSession(timeout=timeout) as session:
                async with session.post(url, json=payload, headers=headers) as resp:
                    if resp.status != 200:
                        text = await resp.text()
                        await self._status(__event_emitter__, f"Hermes error {resp.status}", True)
                        yield f"Hermes returned HTTP {resp.status}: {text[:500]}"
                        return

                    event = None
                    async for raw in resp.content:
                        line = raw.decode("utf-8", "replace").rstrip("\r\n")
                        if not line:
                            event = None
                            continue
                        if line.startswith("event:"):
                            event = line[6:].strip()
                            continue
                        if not line.startswith("data:"):
                            continue
                        data = line[5:].strip()
                        if data == "[DONE]":
                            break
                        try:
                            obj = json.loads(data)
                        except json.JSONDecodeError:
                            continue

                        if event == "hermes.tool.progress":
                            call_id = obj.get("toolCallId") or obj.get("tool", "")
                            if obj.get("status") == "running":
                                label = f"{obj.get('emoji', '🔧')} {obj.get('tool', 'tool')}"
                                if obj.get("label"):
                                    label += f": {obj['label']}"
                                running[call_id] = label
                                await self._status(__event_emitter__, label, False)
                            else:
                                label = running.pop(call_id, obj.get("tool", "tool"))
                                state = obj.get("status", "done")
                                mark = "✓" if state == "completed" else f"({state})"
                                await self._status(__event_emitter__, f"{label} {mark}", True)
                            event = None
                            continue

                        event = None
                        if "error" in obj:
                            err = obj["error"]
                            msg = err.get("message", err) if isinstance(err, dict) else err
                            yield f"\n\n**Hermes error:** {msg}"
                            continue
                        for choice in obj.get("choices", []):
                            chunk = (choice.get("delta") or {}).get("content")
                            if chunk:
                                yield chunk
        except Exception as exc:  # network errors, timeouts
            await self._status(__event_emitter__, "Hermes request failed", True)
            yield f"\n\n**Could not reach Hermes:** {exc}"
            return

        await self._status(__event_emitter__, "Done", True, hidden=True)
