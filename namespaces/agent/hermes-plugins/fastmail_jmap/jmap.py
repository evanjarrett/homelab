"""Minimal async JMAP client (RFC 8620 core, RFC 8621 mail/submission) for the fastmail_jmap plugin.

Only what the adapter needs: session discovery, batched method calls, the EventSource push
stream, blob upload/download. Everything else (what to fetch, what to do with it) lives in
adapter.py. No Hermes imports here, so this module is testable on its own.
"""

from __future__ import annotations

import json
import logging
from typing import Any, AsyncIterator, Optional
from urllib.parse import quote, urlencode

import httpx

logger = logging.getLogger(__name__)

CORE = "urn:ietf:params:jmap:core"
MAIL = "urn:ietf:params:jmap:mail"
SUBMISSION = "urn:ietf:params:jmap:submission"
USING = [CORE, MAIL, SUBMISSION]

DEFAULT_SESSION_URL = "https://api.fastmail.com/jmap/session"


class JmapError(Exception):
    """A request-level or method-level JMAP failure."""

    def __init__(self, message: str, *, kind: str = "", status: int = 0):
        super().__init__(message)
        self.kind = kind  # JMAP error type (``cannotCalculateChanges``) or "" for transport errors
        self.status = status  # HTTP status for transport errors, else 0


class JmapAuthError(JmapError):
    """401/403: the token is wrong, revoked or lacks a scope. Never self-heals."""


def expand_template(template: str, values: dict[str, str]) -> str:
    """Expand the RFC 6570 level-1 ``{var}`` placeholders JMAP URL templates use (values are
    percent-encoded, ``/`` included, as level-1 simple expansion requires)."""
    out = template
    for key, value in values.items():
        out = out.replace("{" + key + "}", quote(str(value), safe=""))
    return out


def event_source_url(template: str, *, types: str = "*", closeafter: str = "no", ping: int = 30) -> str:
    """``eventSourceUrl`` with its ``{types}``/``{closeafter}``/``{ping}`` variables filled. Some
    servers publish a bare URL without the template variables; append them as a query then."""
    values = {"types": types, "closeafter": closeafter, "ping": str(ping)}
    if "{" in template:
        return expand_template(template, values)
    sep = "&" if "?" in template else "?"
    return template + sep + urlencode(values, safe=",*")


def parse_sse_lines(lines: list[str]) -> Optional[tuple[str, str]]:
    """One SSE event (the lines up to a blank line) -> ``(event_type, data)``; ``None`` for a
    comment-only/empty block. ``event`` defaults to ``message`` per the SSE spec."""
    event, data = "message", []
    seen = False
    for line in lines:
        if not line or line.startswith(":"):
            continue
        field, _, value = line.partition(":")
        if value.startswith(" "):
            value = value[1:]
        if field == "event":
            event, seen = value, True
        elif field == "data":
            data.append(value)
            seen = True
    return (event, "\n".join(data)) if seen else None


def changed_types(data: str, account_id: str) -> set[str]:
    """Data types (``Email``, ``Mailbox``...) a JMAP ``StateChange`` push event reports changed for
    *account_id*; empty for anything else (pings, other accounts, junk)."""
    try:
        payload = json.loads(data)
    except (ValueError, TypeError):
        return set()
    if not isinstance(payload, dict) or payload.get("@type") != "StateChange":
        return set()
    changed = payload.get("changed")
    acct = changed.get(account_id) if isinstance(changed, dict) else None
    return set(acct) if isinstance(acct, dict) else set()


class JmapClient:
    """Session + method calls over one ``httpx.AsyncClient``."""

    def __init__(self, token: str, session_url: str = DEFAULT_SESSION_URL, *,
                 client: Optional[httpx.AsyncClient] = None):
        self._token = token
        self._session_url = session_url or DEFAULT_SESSION_URL
        self._client = client or httpx.AsyncClient(timeout=httpx.Timeout(30.0, connect=15.0), follow_redirects=True)
        self.session: dict[str, Any] = {}
        self.account_id = ""

    @property
    def _auth(self) -> dict[str, str]:
        return {"Authorization": f"Bearer {self._token}"}

    async def aclose(self) -> None:
        await self._client.aclose()

    @staticmethod
    def _raise_for(resp: httpx.Response, what: str) -> None:
        if resp.status_code in (401, 403):
            raise JmapAuthError(f"{what}: HTTP {resp.status_code} (check FASTMAIL_API_TOKEN and its scopes)",
                                status=resp.status_code)
        if resp.status_code >= 400:
            raise JmapError(f"{what}: HTTP {resp.status_code}: {resp.text[:300]}", status=resp.status_code)

    async def load_session(self) -> dict[str, Any]:
        """Fetch the session resource and pick the primary mail account (which must also offer
        submission)."""
        resp = await self._client.get(self._session_url, headers=self._auth)
        self._raise_for(resp, "JMAP session")
        session = resp.json()
        caps = session.get("capabilities") or {}
        for cap in (MAIL, SUBMISSION):
            if cap not in caps:
                raise JmapAuthError(f"JMAP session lacks {cap} (token needs Email and Email submission scopes)")
        account_id = (session.get("primaryAccounts") or {}).get(MAIL, "")
        if not account_id:
            raise JmapError("JMAP session has no primary mail account")
        acct_caps = ((session.get("accounts") or {}).get(account_id) or {}).get("accountCapabilities") or {}
        if SUBMISSION not in acct_caps:
            raise JmapAuthError("primary mail account has no submission capability (add the Email submission scope)")
        self.session, self.account_id = session, account_id
        return session

    async def call(self, method_calls: list[list[Any]]) -> list[list[Any]]:
        """POST one JMAP request; returns ``methodResponses``. A top-level failure raises; method
        ``error`` responses are returned for the caller to inspect (see :func:`response`)."""
        if not self.session:
            await self.load_session()
        body = {"using": USING, "methodCalls": method_calls}
        resp = await self._client.post(self.session["apiUrl"], headers={**self._auth, "Content-Type": "application/json"},
                                       content=json.dumps(body))
        self._raise_for(resp, "JMAP API")
        data = resp.json()
        if "methodResponses" not in data:
            raise JmapError(f"JMAP request failed: {str(data)[:300]}", kind=str(data.get("type", "")))
        if self.session.get("state") and data.get("sessionState") and data["sessionState"] != self.session["state"]:
            self.session = {}  # session changed (new urls/capabilities): refetch on next call
        return data["methodResponses"]

    @staticmethod
    def response(responses: list[list[Any]], call_id: str) -> dict[str, Any]:
        """Arguments of the response with *call_id*; raises ``JmapError`` (``kind`` = JMAP error
        type) when that call failed."""
        for name, args, cid in responses:
            if cid != call_id:
                continue
            if name == "error":
                raise JmapError(f"JMAP {call_id}: {args.get('type')}: {args.get('description', '')}",
                                kind=str(args.get("type", "")))
            return args
        raise JmapError(f"JMAP response {call_id} missing")

    async def events(self, *, types: str = "*", ping: int = 30) -> AsyncIterator[tuple[str, str]]:
        """Yield ``(event, data)`` from the push EventSource until the server closes it. The read
        timeout is three ping intervals, so a silently dead stream raises instead of hanging."""
        if not self.session:
            await self.load_session()
        template = self.session.get("eventSourceUrl")
        if not template:
            raise JmapError("JMAP session has no eventSourceUrl")
        url = event_source_url(template, types=types, ping=ping)
        timeout = httpx.Timeout(connect=15.0, read=float(ping * 3), write=15.0, pool=15.0)
        async with self._client.stream("GET", url, headers={**self._auth, "Accept": "text/event-stream",
                                                            "Cache-Control": "no-cache"}, timeout=timeout) as resp:
            self._raise_for(resp, "JMAP EventSource")
            block: list[str] = []
            async for line in resp.aiter_lines():
                if line.strip():
                    block.append(line.rstrip("\r"))
                    continue
                parsed = parse_sse_lines(block)
                block = []
                if parsed is not None:
                    yield parsed

    async def download(self, blob_id: str, name: str, content_type: str, *, max_bytes: int) -> bytes:
        """Fetch a blob; refuses (``JmapError``) anything larger than *max_bytes*."""
        if not self.session:
            await self.load_session()
        url = expand_template(self.session["downloadUrl"], {
            "accountId": self.account_id, "blobId": blob_id, "name": name or "attachment",
            "type": content_type or "application/octet-stream"})
        chunks, total = [], 0
        async with self._client.stream("GET", url, headers=self._auth) as resp:
            self._raise_for(resp, "JMAP download")
            async for chunk in resp.aiter_bytes():
                total += len(chunk)
                if total > max_bytes:
                    raise JmapError(f"blob {blob_id} exceeds {max_bytes} bytes")
                chunks.append(chunk)
        return b"".join(chunks)

    async def upload(self, data: bytes, content_type: str) -> str:
        """Upload *data*; returns the new ``blobId``."""
        if not self.session:
            await self.load_session()
        url = expand_template(self.session["uploadUrl"], {"accountId": self.account_id})
        resp = await self._client.post(url, headers={**self._auth, "Content-Type": content_type or "application/octet-stream"},
                                       content=data)
        self._raise_for(resp, "JMAP upload")
        blob_id = (resp.json() or {}).get("blobId")
        if not blob_id:
            raise JmapError("JMAP upload returned no blobId")
        return blob_id
