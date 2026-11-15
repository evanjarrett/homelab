# agent namespace

Jett, the in-cluster assistant. Being moved from Letta (`10-letta-code.yaml`) to
Hermes Agent + Open WebUI:

| File | What |
|---|---|
| `30-rbac.yaml` | ServiceAccount `jett`: `edit` in `agent`, read across the cluster. Hermes runs as this SA too. |
| `40-quota.yaml` | Namespace ceiling. Letta + Hermes + Open WebUI fit side by side (limits ~7 CPU / 7.5Gi of 8 / 8Gi). |
| `50-hermes.yaml` | Hermes Agent gateway + API server (`hermes:8642`), state on PVC `hermes-data`. |
| `60-open-webui.yaml` | Open WebUI (`open-webui:8080`), state on PVC `open-webui-data`. |
| `20-httproute.yaml` | `agent.j5t.io` -> letta-code, `chat.j5t.io` -> open-webui. |

## Secrets (HSM-synced into `agent`)

| Secret | Keys | Needed by |
|---|---|---|
| `hermes-api-key` | `API_SERVER_KEY` (>=16 chars, `openssl rand -hex 32`) | hermes, open-webui (required) |
| `hermes-platforms` | optional: `DISCORD_BOT_TOKEN`, `DISCORD_ALLOWED_USERS`, `TELEGRAM_BOT_TOKEN`, `TELEGRAM_ALLOWED_USERS`, `SIGNAL_*`, `EMAIL_*` (see `50-hermes.yaml`) | hermes |
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
3. Delete the old untracked chat stack. None of it is in git, so Flux won't do it:
   ```sh
   kubectl -n agent delete httproute agent-gateway-route   # frees chat.j5t.io
   kubectl -n agent delete deploy/agent-gateway svc/agent-gateway
   kubectl -n agent delete pvc agent-gateway-data          # after checking nothing in it is needed
   ```
   Until the route is deleted, the older `agent-gateway-route` keeps winning
   `chat.j5t.io` over `chat-tls-route`.
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
   (`jett-keepalive`, `jett-watcher-config`, `fastmail-*` secrets): review them and
   delete them by hand.
