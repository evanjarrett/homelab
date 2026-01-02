# homelab

## Getting started

```bash
export GITHUB_TOKEN=<token>
flux bootstrap github \
  --token-auth \
  --owner=evanjarrett \
  --repository=homelab \
  --branch=main \
  --path=clusters/homelab \
  --personal \
  --private=false
```