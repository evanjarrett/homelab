# homelab

Flux-managed GitOps for my homelab cluster. The cluster syncs this repo from
`clusters/homelab` and authenticates to GitHub with a **GitHub App** (no PATs).

## Prerequisites

A GitHub App used only for Flux git auth:

1. **Create the App** — GitHub → *Settings → Developer settings → GitHub Apps → New*.
   - **Repository permissions → Contents: Read-only** (Read & write only if you
     later enable image-automation push-back).
   - **Webhook → Active: unchecked**. Leave Callback/Setup URLs blank — Flux
     authenticates machine-to-machine with the private key, so the OAuth/webhook
     fields are unused.
   - **Where can this app be installed?: Only on this account.**
2. **Generate a private key** → downloads a `.pem`. Note the **App ID**.
3. **Install App** → install on `evanjarrett` (the repo owner). Without an
   installation, auth fails with a `404 .../installation` lookup error.

You need: the **App ID**, the **`.pem`**, and the owner (`evanjarrett`). The
installation ID is auto-resolved from the owner on Flux ≥ 2.8.

## Fresh deploy

```bash
# 1. Namespace + GitHub App auth secret (out-of-band; the .pem is never committed)
kubectl create namespace flux-system
flux create secret githubapp flux-system \
  --namespace=flux-system \
  --app-id=<APP_ID> \
  --app-installation-owner=evanjarrett \
  --app-private-key=./evan-homelab-flux.private-key.pem

# 2. Apply Flux controllers + sync manifests from this repo
kubectl apply -k clusters/homelab/flux-system

# 3. Confirm the source authenticates and the cluster converges
flux get sources git flux-system        # READY=True
flux get kustomizations -A
```

The secret must exist **before** the GitRepository reconciles, since
`gotk-sync.yaml` already sets `spec.provider: github`.

## Rotating the App private key

```bash
flux create secret githubapp flux-system \
  --namespace=flux-system \
  --app-id=<APP_ID> \
  --app-installation-owner=evanjarrett \
  --app-private-key=./new-key.pem
flux reconcile source git flux-system
```

## Migrating an existing PAT bootstrap to the App (one-time)

Switching an already-bootstrapped `flux-system` from token auth to the App hits a
chicken-and-egg ([fluxcd/flux2#5471](https://github.com/fluxcd/flux2/issues/5471)):
source-controller won't fetch the commit that adds `provider: github` because the
*live* object still lacks `provider`. Break the deadlock by suspending the
Kustomization while you patch the live object:

```bash
# Secret already swapped via `flux create secret githubapp` above, and
# `provider: github` committed to gotk-sync.yaml.
flux suspend kustomization flux-system
kubectl -n flux-system patch gitrepository flux-system \
  --type=merge -p '{"spec":{"provider":"github"}}'
flux reconcile source git flux-system     # now authenticates with the App
flux resume kustomization flux-system
```
