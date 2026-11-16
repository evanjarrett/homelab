# privacy-gate

An OpenAI-compatible proxy that sits behind the semantic router's `claude` model and decides
whether a request may leave the house. Only **shadow mode** is implemented: nothing is sent to
Anthropic, every request is answered by llama-server, and the gate records what it *would*
have done.

```
client -> llm-gateway (Envoy) --ext_proc--> semantic-router
             | x-selected-model: claude   (keywords / >=128K tokens / complexity hard)
             v
        privacy-gate :8080
             | sensitive or error  -> llama-server (model qwen3.8-27b)  x-privacy-gate: local; reason=...
             | clean (shadow)      -> llama-server                      x-privacy-gate: shadow-local
             | clean (enforce)     -> NOT WIRED: falls back to local, reason=cloud_unwired
```

Files: `privacy_gate.py` (server, store, classifier), `rules.py` (deterministic detectors),
`test_privacy_gate.py` (offline tests). Deployed by `../40-privacy-gate.yaml` and the
GENERATED ConfigMap `../41-privacy-gate-code.yaml`. Python stdlib only, running on the stock
`python:3.13.18-slim` image (multi-arch, pinned by digest). Nothing to build.

## Per request

1. **Conversation id**: the first non-empty header of `GATE_ID_HEADERS`
   (`x-conversation-id`, `x-openwebui-chat-id`, `x-session-id`). Hermes sends
   `x-conversation-id` (its session key, via `providers.llm-gateway.session_affinity_header` in
   `../../agent/50-hermes.yaml`). Open WebUI sends `X-OpenWebUI-Chat-Id`
   (`ENABLE_FORWARD_USER_INFO_HEADERS`, `../../agent/60-open-webui.yaml`). With no header the id
   is `sha256(system prompt + first user message)`. That fingerprint is weak: unrelated chats
   that open the same way share an id (too much taint, the safe direction), and a client that
   rewrites its system prompt every turn gets a new id every turn (the taint is lost). Step 3
   still scans the whole conversation on every request, so the taint is a second line of
   defence. Only `sha256("hdr"|name|value)` or the fingerprint hash is stored. Logs show its
   last 8 hex characters.
2. **Taint store**: SQLite at `/data/privacy-gate.sqlite3` (1Gi longhorn-v2 PVC). A tainted id
   goes local at once, with no detection. The TTL slides: it is refreshed on every hit and is
   30 days by default (`GATE_TAINT_TTL_DAYS`). The store also keeps, per conversation, a digest
   of the message prefix that was already classified clean, so the classifier only sees *new*
   messages. If the history is edited or compressed, the whole conversation is classified again.
3. **Detection** runs over every message in every role: text, assistant tool calls with their
   arguments, tool results, and reasoning fields. Tool *definitions* are excluded.
   - **Rules** (`rules.py`, run on the text and on its percent-decoded and base64-decoded
     forms):
     - contact data: `email`, `phone`
     - payment and identity numbers: `card` (Luhn check), `iban` (mod-97 check), `ssn`
     - personal details: `address`, `dob` (needs a keyword), `private_ip` (RFC 1918, fc00::/7)
     - keys and tokens: `private_key`, `jwt`, `cloud_key`, `vcs_token`, `api_token`, `bearer`
     - other credentials: `url_credentials`, `password_assign` (placeholders ignored)
     - `marker` ("confidential", "do not share", ...)

     Any hit makes the request sensitive. `GATE_RULES_DISABLED` turns codes off, for example
     `private_ip`, which most homelab tool output will trigger.
   - **Media**: any image, audio or file part makes the request sensitive (`media`).
   - **LLM classifier** (only when no rule fired, because it can only raise the verdict): the
     gate POSTs directly to `vllm-vision:8080` (`vision`, Qwen3.5-9B) and never through the
     router, so there are no loops. The request uses temperature 0, `max_tokens` 96, and a
     vLLM `structured_outputs.regex` that forces compact
     `{"sensitive":bool,"categories":[...],"reason":"..."}`. Plain `json_schema` let the model
     loop on whitespace until `max_tokens` on ~6K-token inputs. The system prompt lists the
     implicit categories: health, finances, legal, relationships, children_family,
     precise_location, employment_hr, credentials, identity_documents, contact_details and
     other_personal. It treats the excerpt as data, and says "when unsure, sensitive".
     A category without `sensitive: true` still counts as sensitive. `reason` is discarded,
     because it can quote content.
   - **Chunking**: the unclassified messages are packed in order into chunks of
     `GATE_CLASSIFIER_CHUNK_TOKENS` × 3 chars (12000 tokens at a conservative 3 chars/token,
     which fits vision's 32K window). A single long message is split. There are at most
     `GATE_CLASSIFIER_MAX_CHUNKS` (4) chunks, classified two at a time and OR-ed together.
     Nothing is truncated: content beyond the chunk budget fails closed.
4. **Fail closed**: each of these makes the request sensitive and keeps it local:
   - classifier timeout (20s per call: `clf_timeout`) or total budget exceeded (45s: `clf_budget`)
   - connection or HTTP error, or output that cannot be parsed
   - content too large to classify (`clf_overflow`)
   - classifier disabled (`clf_disabled`)
   - any exception inside the gate (`gate_error`)
5. **Sensitive**: taint the id, forward to llama-server with `model` rewritten to
   `qwen3.8-27b`, and send `x-privacy-gate: local; reason=<codes>`.
6. **Clean**: shadow mode logs it, counts `privacy_gate_would_escalate_total`, forwards to
   llama-server, and sends `x-privacy-gate: shadow-local`.

Forwarding to upstreams passes the body through untouched. The only change is the `model`
field. SSE streams are relayed byte for byte as they arrive, re-chunked. Only `content-type`,
`accept` and `x-request-id` are forwarded upstream. Client identity headers such as
`X-OpenWebUI-User-*` and the conversation ids never are.

Measured against the live vision model (synthetic texts):

| Input | Latency |
|---|---|
| Short prompt, 1 call | 1.4–1.7s |
| ~10K tokens, 2 chunks | ~6.7s |

On the synthetic texts, health, finance, family and divorce prompts were flagged. A technical
question, a story about a dragon and a large kubectl log were not. A health remark at the end
of a long log was flagged.

## Logs and metrics

The gate writes one JSON line per request to stdout and never logs prompt text:
`conv` (hash suffix), `id_src`, `route`, `verdict`, `reasons`, `clf_categories`, `clf_chunks`,
`clf_skipped_messages`, `newly_tainted`, `tokens_est`, `stream`, `status`, `detect_ms`,
`clf_ms` and `total_ms`.

The Prometheus endpoint is `/metrics` on :8080. The Service is annotated for VictoriaMetrics.

- `privacy_gate_requests_total{route,reason}` (`route` is `local` or `shadow_local`)
- `privacy_gate_would_escalate_total`
- `privacy_gate_tainted_conversations_total`, gauge `privacy_gate_tainted_conversations`
- `privacy_gate_rule_hits_total{code}`
- `privacy_gate_classifier_verdicts_total{verdict}`
- `privacy_gate_classifier_latency_seconds`
- `privacy_gate_errors_total{kind}`

Useful queries:
`sum by (route, reason) (increase(privacy_gate_requests_total[1d]))` and
`increase(privacy_gate_would_escalate_total[1d])`. On the router side:
`sum by (decision_name) (increase(llm_decision_match_total[1d]))`.

## Enforce mode (not wired)

`GATE_MODE=enforce` refuses to start without `ANTHROPIC_API_KEY`. That key comes from the
optional Secret `anthropic-api-key`, key `ANTHROPIC_API_KEY`. `CloudUpstream` is a stub, so
even with a key every clean request currently falls back to local with
`reason=cloud_unwired`. To wire it, pick one of these:

1. **Anthropic's OpenAI-compatible endpoint** (`https://api.anthropic.com/v1/chat/completions`).
   Rewrite `model` (`ANTHROPIC_MODEL`, default `claude-opus-5-5`) and send the key. This is the
   least code, because streaming SSE and tool calls stay in OpenAI shape. The drawback: it is a
   compatibility layer, so some request fields are ignored or rejected, and Claude-specific
   features (prompt caching, thinking control) are limited.
2. **Translate to the Messages API** (`/v1/messages`, ideally with the official `anthropic`
   SDK, which means a real image instead of a stock one). This gives full features and prompt
   caching, which matters for 20K-token Hermes turns. The cost is translating messages, tools
   and SSE events in both directions.

Before enabling either option:

- strip client headers (already done for upstreams)
- decide whether `private_ip` and similar codes should keep blocking
- test streaming and tool-call round trips
- add an egress NetworkPolicy for api.anthropic.com (the namespace already allows :443 out)

## Develop

```sh
cd namespaces/llm/privacy-gate
python3 -m unittest -v          # stdlib only, offline (loopback fake upstreams)
cd .. && kubectl create configmap privacy-gate-code -n llm \
  --from-file=privacy-gate/privacy_gate.py --from-file=privacy-gate/rules.py \
  --dry-run=client -o yaml | cat privacy-gate/configmap-header.txt - > 41-privacy-gate-code.yaml
# after Flux applies it:
kubectl -n llm rollout restart deploy/privacy-gate
```

Configuration is all `GATE_*` env in `../40-privacy-gate.yaml`. Defaults are in
`Config` in `privacy_gate.py`.
