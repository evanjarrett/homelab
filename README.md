# homelab


## Getting started
export GITHUB_TOKEN=<token>
flux bootstrap github \
  --token-auth \
  --owner=evanjarrett \
  --repository=homelab \
  --branch=main \
  --path=kubes \
  --personal
  --private=false