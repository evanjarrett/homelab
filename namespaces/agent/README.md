# agent namespace

Jett, the in-cluster assistant. Being moved from Letta (`10-letta-code.yaml`) to
Hermes Agent + Open WebUI:

| File | What |
|---|---|
| `30-rbac.yaml` | ServiceAccount `jett`: `edit` in `agent`, read across the cluster. Hermes runs as this SA too. |
| `40-quota.yaml` | Namespace ceiling. Letta + Hermes + Open WebUI fit side by side (limits ~7 CPU / 7.5Gi of 8 / 8Gi). |
| `50-hermes.yaml` | Hermes Agent gateway + API server (`hermes:8642`), state on PVC `hermes-data`. |
| `51-hermes-fastmail-plugin.yaml` | GENERATED ConfigMap with the `fastmail_jmap` Hermes plugin (source: `hermes-plugins/fastmail_jmap/`). |
| `60-open-webui.yaml` | Open WebUI (`open-webui:8080`), state on PVC `open-webui-data`. |
| `20-httproute.yaml` | `agent.j5t.io` -> letta-code, `chat.j5t.io` -> open-webui. |

## Secrets (HSM-synced into `agent`)

| Secret | Keys | Needed by |
|---|---|---|
| `hermes-api-key` | `API_SERVER_KEY` (>=16 chars, `openssl rand -hex 32`) | hermes, open-webui (required) |
| `hermes-platforms` | optional: `DISCORD_BOT_TOKEN`, `DISCORD_ALLOWED_USERS`, `TELEGRAM_BOT_TOKEN`, `TELEGRAM_ALLOWED_USERS`, `SIGNAL_*`, `EMAIL_*` (see `50-hermes.yaml`) | hermes |
| `hermes-fastmail` | optional: `FASTMAIL_API_TOKEN`, `FASTMAIL_ADDRESS`, `FASTMAIL_ALLOWED_USERS`, `FASTMAIL_AUTHSERV_ID`, `FASTMAIL_HOME_ADDRESS`, `FASTMAIL_SESSION_URL` (see "Emailing the agent") | hermes |
| `open-webui-secret` | optional: `WEBUI_SECRET_KEY` | open-webui |

Create `hermes-api-key` **before** the first deploy. Otherwise Hermes generates its
own key into `/opt/data/.env` (which then wins over the env), and Open WebUI's
connection key is wrong. Open WebUI also saves connection settings to its DB on
first boot.

## Cutover from Letta

1. Create the `hermes-api-key` HSMSecret. Merge this branch and let Flux reconcile.
   letta-code keeps running alongside.
2. Check Hermes: `kubectl -n agent logs deploy/hermes` (look for the API server
   listening on 0.0.0.0:8642), then
   `kubectl -n agent exec deploy/hermes -- curl -s localhost:8642/health`.
3. ~~Delete the old untracked chat stack~~ — done 2026-11-15: `agent-gateway`
   (Deployment, Service, PVC `agent-gateway-data`, HTTPRoute `agent-gateway-route`),
   `jett-watcher-config` and the `fastmail-api-token` / `fastmail-jmap-token`
   Secrets were deleted. chat.j5t.io now serves Open WebUI.
4. Open https://chat.j5t.io right away and create the admin account (the first
   sign-up becomes admin). Then set `ENABLE_SIGNUP=false` or approve users by hand.
5. Carry over anything worth keeping from Letta (memory blocks / memfs repos on
   `letta-code-data`, the `jett-config` / `jett-secrets` objects) into Hermes
   memory, skills or `hermes-platforms` by hand. There is no automatic migration.
6. Retire Letta in git: delete `10-letta-code.yaml` and the `agent-tls-route` in
   `20-httproute.yaml`. Flux prunes the Deployment, Service, PVC and route. Then
   delete the HSMSecret `letta-ws-token`, update `30-rbac.yaml` /
   `00-namespace.yaml` comments that mention letta-code, and the baseline in
   `40-quota.yaml`.
7. Leftover live objects not in git that Letta-era Jett created
   (`jett-keepalive`, if still present): review and delete by hand.

## Seeing Hermes tool calls in Open WebUI

Hermes reports tool activity as custom `event: hermes.tool.progress` SSE frames,
which Open WebUI's plain OpenAI connection drops, so the `jett` model only shows
the final answer. `open-webui-functions/hermes_pipe.py` is an Open WebUI Pipe that
calls Hermes itself and turns those frames into live status lines
("💻 terminal: hostname + 1 command" → "✓").

Install (needs the Open WebUI admin account): Admin Panel → Functions → **+** →
paste the file → Save → toggle it on. It shows up as **Jett (Hermes Agent)**. It
reads the Hermes key from the pod's `OPENAI_API_KEYS`, so no valve needs setting.
Optionally hide the raw `jett` model (Admin Panel → Settings → Models) to avoid
two entries. Open WebUI keeps functions in its database: re-paste after edits.

## Emailing the agent (Fastmail JMAP plugin)

`hermes-plugins/fastmail_jmap/` is a Hermes messaging-platform plugin: mail that lands
in the Inbox of the agent's (dedicated) Fastmail account becomes a Hermes chat, and the
answer goes back as a threaded reply. Chat = sender address, thread = the JMAP thread, so
a new email starts a conversation and replying in the thread continues it.

How it runs:

- The ConfigMap `hermes-fastmail-plugin` is mounted at `/etc/hermes-plugins/fastmail_jmap`;
  the container's startup script copies it (minus `test_*`) into
  `$HERMES_HOME/plugins/fastmail_jmap` on every start, uid 10000-owned. User plugins are
  opt-in, so the seed config lists it under `plugins.enabled`.
- The platform (`fastmail`) only turns on when `FASTMAIL_API_TOKEN`, `FASTMAIL_ADDRESS`
  and a non-empty `FASTMAIL_ALLOWED_USERS` are all set. There is no allow-all.
- Inbound: JMAP push (EventSource, `closeafter=no`, 30s ping, reconnect with backoff)
  wakes a sync, and a 60s timer runs the same sync in case push drops. A sync is
  `Email/changes` since the stored Email state, or `Email/query` on
  `cannotCalculateChanges`. State lives in
  `$HERMES_HOME/platforms/fastmail/state-<address>.json` (Email state, the processed ids,
  and reply threading context). On the first start it only records a baseline, so mail
  that was already there is never answered.
- Mail is handled only when all of these hold. Anything else is ignored: no reply, left
  unread, logged at INFO with the reason.
  - The From: is exactly one address. It is not one of the agent's own identities.
  - The mail is not automated: Auto-Submitted, Precedence bulk/list/junk, List-Id,
    List-Unsubscribe, X-Auto-Response-Suppress, a `<>` Return-Path, or a
    noreply/mailer-daemon/postmaster/bounce address all count as automated.
  - The From: address is listed in `FASTMAIL_ALLOWED_USERS`.
  - The sender is authenticated (next item).
- Sender authentication reuses the hardened checks from Hermes's bundled IMAP email
  plugin (GHSA-rxqh-5572-8m77). Only the topmost `Authentication-Results` header counts,
  because the receiving MTA prepends its own. Its authserv-id must be trusted, and it must
  show DMARC pass, or SPF or DKIM pass aligned with the From: domain.
- Delivery is at-most-once. The message is recorded as processed, handed to the agent,
  then marked `$seen`. A crash at the wrong moment drops that one email rather than
  running the agent's tools twice.
- Replies are sent with `Email/set` (a draft in Drafts) plus `EmailSubmission/set`. The
  identity comes from `Identity/get` matching `FASTMAIL_ADDRESS`. On success the server
  moves the draft to Sent. Replies carry `In-Reply-To`/`References`, a `Re:` subject and
  `Auto-Submitted: auto-replied` (RFC 3834) so other robots don't answer back.
  The one recipient is the authenticated sender. Reply-To, Cc and addresses inside
  forwarded content are never used. Outbound mail of any kind (including the agent's
  `send_message` and cron) only goes to `FASTMAIL_ALLOWED_USERS` or
  `FASTMAIL_HOME_ADDRESS`.
- Forwards are supported in two styles:
  - Inline ("---------- Forwarded message ---------", "Begin forwarded message:",
    "Original Message").
  - As an attachment (`message/rfc822` / `.eml`).

  The agent gets the sender's own text first, as the request. Each forwarded message
  follows in a fenced `FORWARDED_EMAIL` block (From/Date/Subject/To + body, truncated to
  20k characters) marked as untrusted data. Other attachments (up to 10, 20 MB each)
  are passed through as files.

**Forwarding rules.** A Fastmail *redirect* rule (or any "redirect/bounce") keeps the
original sender in From:, so the allow-list ignores it. Forward by hand, or use a rule
that *forwards* (re-sends the mail from your own address, as an attachment or inline).
Then From: is you, authenticated by your own domain's DMARC.

### Setup

1. **API token.** Log in to Fastmail as the agent's account. Go to Settings → Privacy &
   Security → Integrations → **API tokens** (older UI: "Connected apps & API tokens") →
   *New API token*. Give it access to **Email** (not read-only) and **Email submission**.
   Nothing else is needed. `FASTMAIL_ADDRESS` must be a sending identity of that account
   (Settings → Mail → Sending identities).
2. **Secret** (the HSMSecret entry `hermes-fastmail` → namespace `agent` is already in
   `infrastructure/hsm-secrets-operator/00-release.yaml`):

   ```sh
   kubectl hsm create hermes-fastmail -n secrets --interactive
   #   FASTMAIL_API_TOKEN      fmu1-...
   #   FASTMAIL_ADDRESS        agent@example.com          (the agent's address)
   #   FASTMAIL_ALLOWED_USERS  jett@example.com,other@example.com   (lowercase!)
   #   FASTMAIL_HOME_ADDRESS   jett@example.com           (optional: cron/notifications)
   #   FASTMAIL_AUTHSERV_ID    .messagingengine.com       (optional, see below)
   ```

   Write the addresses in lowercase. The gateway's own allow-list check compares them
   case-sensitively against the lowercased sender. The Secret is an optional `envFrom`,
   so the pod runs without it. Restart the pod after creating or changing it:
   `kubectl -n agent rollout restart deploy/hermes`.
3. Check: `kubectl -n agent logs deploy/hermes | grep -i fastmail` should show
   `[Fastmail] baseline set` once, then `connected as ...`. Send a test mail from an
   allowed address. If it is ignored, the INFO line says why.

### Finding FASTMAIL_AUTHSERV_ID

When it is unset, the plugin trusts `.messagingengine.com`: any `mxN.messagingengine.com`
authserv-id. Fastmail's inbound MX hosts each stamp their own hostname, so one exact
value would randomly drop mail. To check: open a message the agent account received from
outside (Fastmail web → message → ⋯ → *Show raw message*). Take the **topmost**
`Authentication-Results:` header. The authserv-id is the first token before the `;`,
e.g. `Authentication-Results: mx4.messagingengine.com;`. Set a comma list of exact ids
and/or `.suffix` entries to override.

The suffix is no weaker than an exact pin. Neither one proves who wrote the header: the
protection is that only the topmost header is read, and Fastmail always prepends its own.
Mail between two Fastmail accounts is the case to check once. If such mail is dropped
with "no Authentication-Results" or "not trusted", look at its raw topmost header and
adjust.

### Changing the plugin

Edit `hermes-plugins/fastmail_jmap/`, run the offline tests (below), then regenerate the
ConfigMap (from `namespaces/agent/`):

```sh
kubectl create configmap hermes-fastmail-plugin -n agent --from-file=hermes-plugins/fastmail_jmap/ \
  --dry-run=client -o yaml | cat hermes-plugins/configmap-header.txt - > 51-hermes-fastmail-plugin.yaml
```

`hermes-plugins/kustomization.yaml` is an empty kustomization. It stops Flux's
auto-generated kustomization from trying to apply `plugin.yaml` as a manifest.

Tests (offline, from the mounted copy, with the Hermes venv; they write only under /tmp):

```sh
kubectl -n agent exec deploy/hermes -c hermes -- sh -c '
  rm -rf /tmp/fmtest && mkdir -p /tmp/fmtest/fastmail_jmap /tmp/fmtest/home &&
  cp -L /etc/hermes-plugins/fastmail_jmap/* /tmp/fmtest/fastmail_jmap/ &&
  cd /opt/hermes && HERMES_HOME=/tmp/fmtest/home .venv/bin/python /tmp/fmtest/fastmail_jmap/test_fastmail_jmap.py;
  rc=$?; rm -rf /tmp/fmtest; exit $rc'
```
