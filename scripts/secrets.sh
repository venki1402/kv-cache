#!/usr/bin/env bash
# Creates the app namespace and a random AUTH_TOKEN secret. The token is
# generated in-cluster only and never written to the repo.
set -euo pipefail
NS=kv-cache
kubectl create namespace "$NS" --dry-run=client -o yaml | kubectl apply -f -
kubectl label namespace "$NS" --overwrite \
  pod-security.kubernetes.io/enforce=restricted \
  pod-security.kubernetes.io/warn=restricted
if kubectl -n "$NS" get secret kv-cache-auth >/dev/null 2>&1; then
  echo "secret kv-cache-auth already exists"
else
  kubectl -n "$NS" create secret generic kv-cache-auth --from-literal=token="$(openssl rand -hex 24)"
  echo "created secret kv-cache-auth"
fi
echo "read the token with:"
echo "  kubectl -n $NS get secret kv-cache-auth -o jsonpath='{.data.token}' | base64 -d"
