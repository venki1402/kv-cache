#!/usr/bin/env bash
# Installs the platform components that live outside Argo CD, with chart
# versions pinned in deploy/platform/versions.env.
set -euo pipefail
cd "$(dirname "$0")/.."
source deploy/platform/versions.env
V=deploy/platform

helm repo add traefik "$TRAEFIK_REPO" --force-update >/dev/null
helm repo add prometheus-community "$KPS_REPO" --force-update >/dev/null
helm repo add jaegertracing "$JAEGER_REPO" --force-update >/dev/null
helm repo add argo "$ARGOCD_REPO" --force-update >/dev/null
helm repo add grafana "$GRAFANA_REPO" --force-update >/dev/null
helm repo add metrics-server "$METRICS_SERVER_REPO" --force-update >/dev/null
helm repo update >/dev/null

install() { # release namespace chart version values
  helm upgrade --install "$1" "$3" --version "$4" -n "$2" --create-namespace -f "$5" --wait --timeout 10m
}

install metrics-server kube-system "$METRICS_SERVER_CHART" "$METRICS_SERVER_CHART_VERSION" $V/metrics-server-values.yaml
# Traefik first: every other chart's Ingress needs its IngressClass.
install traefik traefik "$TRAEFIK_CHART" "$TRAEFIK_CHART_VERSION" $V/traefik-values.yaml

kubectl create namespace monitoring --dry-run=client -o yaml | kubectl apply -f -
if ! kubectl -n monitoring get secret grafana-admin >/dev/null 2>&1; then
  kubectl -n monitoring create secret generic grafana-admin \
    --from-literal=admin-user=admin \
    --from-literal=admin-password="$(openssl rand -base64 18)"
fi
install kps monitoring "$KPS_CHART" "$KPS_CHART_VERSION" $V/kube-prometheus-stack-values.yaml
install loki monitoring "$LOKI_CHART" "$LOKI_CHART_VERSION" $V/loki-values.yaml
install alloy monitoring "$ALLOY_CHART" "$ALLOY_CHART_VERSION" $V/alloy-values.yaml
install jaeger observability "$JAEGER_CHART" "$JAEGER_CHART_VERSION" $V/jaeger-values.yaml
install argocd argocd "$ARGOCD_CHART" "$ARGOCD_CHART_VERSION" $V/argocd-values.yaml

kubectl get pods -A --field-selector=status.phase!=Running,status.phase!=Succeeded
