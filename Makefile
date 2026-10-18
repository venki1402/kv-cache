SHELL := /usr/bin/env bash
CLUSTER   ?= kvcache
NS        ?= kv-cache
INGRESS   ?= http://kv.localhost:8080
IMAGE     ?= ghcr.io/venki1402/kv-cache
SHA       := $(shell git rev-parse HEAD)

.PHONY: up cluster-up platform-up secrets argo-app dev-deploy smoke load fault-demo drift-demo evidence urls down test lint chart-check bench

up: cluster-up platform-up secrets argo-app urls

cluster-up:
	@kind get clusters | grep -qx $(CLUSTER) || kind create cluster --config deploy/kind/cluster.yaml --wait 120s
	kubectl config use-context kind-$(CLUSTER)
	@# Lets workloads run on the control plane too, so draining the worker has
	@# somewhere to reschedule the pod.
	-kubectl taint nodes $(CLUSTER)-control-plane node-role.kubernetes.io/control-plane:NoSchedule-
	kind version; kubectl version | grep -i server

platform-up:
	scripts/platform-up.sh

secrets:
	scripts/secrets.sh

argo-app:
	kubectl apply -f deploy/argocd/application.yaml

# Fast inner loop: build locally and load into kind, bypassing GHCR and Argo CD.
dev-deploy:
	docker build -t $(IMAGE):$(SHA) --build-arg VERSION=$$(git describe --tags --always --dirty) \
	  --build-arg COMMIT=$(SHA) --build-arg BUILD_TIME=$$(date -u +%FT%TZ) .
	kind load docker-image $(IMAGE):$(SHA) --name $(CLUSTER)
	helm upgrade --install kv-cache deploy/helm/kv-cache -n $(NS) \
	  -f deploy/helm/kv-cache/values-local.yaml --set image.tag=$(SHA) --wait

smoke:
	AUTH_TOKEN=$$(kubectl -n $(NS) get secret kv-cache-auth -o jsonpath='{.data.token}' | base64 -d) \
	EXPECTED_SHA=$$(kubectl -n $(NS) get deploy kv-cache -o jsonpath='{.spec.template.spec.containers[0].image}' | cut -d: -f2) \
	BASE_URL=$(INGRESS) scripts/smoke.sh

load:
	scripts/load.sh

fault-demo:
	scripts/fault-demo.sh

drift-demo:
	scripts/drift-demo.sh

evidence:
	scripts/evidence.sh

urls:
	@echo "app          $(INGRESS)/version"
	@echo "grafana      http://grafana.localhost:8080   (user/pass: kubectl -n monitoring get secret grafana-admin -o go-template='{{index .data \"admin-user\" | base64decode}} {{index .data \"admin-password\" | base64decode}}')"
	@echo "prometheus   http://prometheus.localhost:8080"
	@echo "alertmanager http://alertmanager.localhost:8080"
	@echo "jaeger       http://jaeger.localhost:8080"
	@echo "argocd       http://argocd.localhost:8080   (admin / kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d)"

down:
	kind delete cluster --name $(CLUSTER)

test:
	go test -race -cover ./...

lint:
	go vet ./...
	golangci-lint run ./...

chart-check:
	for env in local prod; do \
	  helm lint deploy/helm/kv-cache -f deploy/helm/kv-cache/values-$$env.yaml && \
	  helm template kv-cache deploy/helm/kv-cache -n $(NS) -f deploy/helm/kv-cache/values-$$env.yaml | \
	    kubeconform -strict -summary -schema-location default \
	      -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' || exit 1; \
	done

bench:
	scripts/bench.sh
