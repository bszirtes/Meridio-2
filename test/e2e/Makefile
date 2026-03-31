.PHONY: default
default: test

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-30s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } END { printf "\n" }' $(MAKEFILE_LIST)

################################################################################
# Variables
################################################################################

SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

# Inherit from parent or set defaults
REGISTRY ?= registry.nordix.org/cloud-native/meridio-2
VERSION ?= latest
KUBECTL ?= kubectl
KIND ?= kind

# E2E specific
CLUSTER_NAME ?= kind
KUBERNETES_WORKERS ?= 4
BREAKOUT_NETWORK ?= kind-breakout

# Tool versions
MULTUS_VERSION ?= v4.1.2
WHEREABOUTS_VERSION ?= v0.8.0

# Suites
SUITES ?= common-appnetwork separate-appnetwork

# Paths
PROJECT_ROOT := $(shell cd ../.. && pwd)
LOCALBIN ?= $(PROJECT_ROOT)/bin
KUSTOMIZE ?= $(LOCALBIN)/kustomize

################################################################################
##@ Cluster Setup
################################################################################

.PHONY: cluster
cluster: kustomize multus cni-plugins whereabouts cert-manager gateway-api kind-network vpn-gateway ## Setup cluster dependencies.

.PHONY: kustomize
kustomize: ## Install kustomize from parent Makefile.
	$(MAKE) -C $(PROJECT_ROOT) kustomize

.PHONY: multus
multus: ## Install Multus CNI.
	@$(KUBECTL) get daemonset -n kube-system kube-multus-ds >/dev/null 2>&1 || { \
		$(KUBECTL) apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/$(MULTUS_VERSION)/deployments/multus-daemonset.yml; \
		$(KUBECTL) rollout status daemonset kube-multus-ds -n kube-system --timeout=300s; \
	}

.PHONY: cni-plugins
cni-plugins: ## Install CNI plugins.
	@$(KUBECTL) get daemonset -n kube-system install-cni-plugins >/dev/null 2>&1 || { \
		curl -sL https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/master/e2e/templates/cni-install.yml.j2 | \
		sed '/^\s*wget / s/$$/ || { echo "wget failed"; exit 1; }/' | \
		$(KUBECTL) apply -f -; \
		$(KUBECTL) rollout status daemonset install-cni-plugins -n kube-system --timeout=300s; \
	}

.PHONY: whereabouts
whereabouts: ## Install Whereabouts IPAM.
	@$(KUBECTL) get daemonset -n kube-system whereabouts >/dev/null 2>&1 || { \
		$(KUBECTL) apply \
		-f https://raw.githubusercontent.com/k8snetworkplumbingwg/whereabouts/$(WHEREABOUTS_VERSION)/doc/crds/daemonset-install.yaml \
		-f https://raw.githubusercontent.com/k8snetworkplumbingwg/whereabouts/$(WHEREABOUTS_VERSION)/doc/crds/whereabouts.cni.cncf.io_ippools.yaml \
		-f https://raw.githubusercontent.com/k8snetworkplumbingwg/whereabouts/$(WHEREABOUTS_VERSION)/doc/crds/whereabouts.cni.cncf.io_overlappingrangeipreservations.yaml; \
		$(KUBECTL) rollout status daemonset whereabouts -n kube-system --timeout=300s; \
	}

.PHONY: gateway-api
gateway-api: ## Install Gateway API CRDs.
	@$(KUBECTL) get crd gateways.gateway.networking.k8s.io >/dev/null 2>&1 || \
		$(KUBECTL) apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.5.0/standard-install.yaml

.PHONY: cert-manager
cert-manager: ## Install cert-manager.
	@$(KUBECTL) get deployment -n cert-manager cert-manager >/dev/null 2>&1 || { \
		$(KUBECTL) apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.2/cert-manager.yaml; \
		$(KUBECTL) wait --for=condition=Available --timeout=300s -n cert-manager deployment/cert-manager; \
		$(KUBECTL) wait --for=condition=Available --timeout=300s -n cert-manager deployment/cert-manager-webhook; \
		$(KUBECTL) wait --for=condition=Available --timeout=300s -n cert-manager deployment/cert-manager-cainjector; \
	}

.PHONY: kind-network
kind-network: ## Setup kind bridge network.
	@docker network inspect $(BREAKOUT_NETWORK) >/dev/null 2>&1 || { \
		docker network create $(BREAKOUT_NETWORK) --driver=bridge \
			--subnet=172.19.0.0/16 \
			--gateway=172.19.0.1 \
			--ipv6 --subnet=fc00:dead:beef::/48 \
			--opt com.docker.network.bridge.name=br-meridio; \
		for i in $$(seq 0 $(KUBERNETES_WORKERS)); do \
			node=$$($(KIND) get nodes --name $(CLUSTER_NAME) | sed -n "$$((i+1))p"); \
			[ -n "$$node" ] && docker network connect $(BREAKOUT_NETWORK) $$node || true; \
		done; \
	}

.PHONY: vpn-gateway
vpn-gateway: ## Deploy VPN gateway container.
	@docker ps --filter name=vpn-gateway --format '{{.Names}}' | grep -q vpn-gateway || { \
		cd $(PROJECT_ROOT)/hack/vpn-gateway && \
		docker compose build && \
		docker compose up -d; \
	}

.PHONY: cluster-cleanup
cluster-cleanup: ## Cleanup cluster dependencies.
	cd $(PROJECT_ROOT)/hack/vpn-gateway && docker compose down 2>/dev/null || true
	docker network rm $(BREAKOUT_NETWORK) 2>/dev/null || true

################################################################################
##@ Suite: common-appnetwork
################################################################################

.PHONY: common-appnetwork
common-appnetwork: deploy-common-appnetwork test-common-appnetwork ## Deploy and test common-appnetwork suite.

.PHONY: deploy-common-appnetwork
deploy-common-appnetwork: cluster ## Deploy common-appnetwork topology.
	@NS=e2e-common-appnet; \
	SUITE_DIR=$(shell pwd)/suites/common-appnetwork; \
	$(KUBECTL) create namespace $$NS --dry-run=client -o yaml | $(KUBECTL) apply -f -; \
	$(KUBECTL) label namespace $$NS e2e-suite=common-appnet --overwrite; \
	$(KUBECTL) apply -f suites/common/gatewayclass.yaml; \
	sed 's|registry.nordix.org/cloud-native/meridio-2|$(REGISTRY)|g; s|:latest|:$(VERSION)|g' \
		$(PROJECT_ROOT)/config/templates/lb-deployment.yaml | \
		$(KUBECTL) create configmap lb-template-override-common-appnet --from-file=lb-deployment.yaml=/dev/stdin -n $$NS --dry-run=client -o yaml | \
		$(KUBECTL) apply -f -; \
	cd $$SUITE_DIR && $(KUSTOMIZE) edit set image controller-manager=$(REGISTRY)/controller-manager:$(VERSION); \
	cd $$SUITE_DIR && $(KUSTOMIZE) build . | $(KUBECTL) apply -f -; \
	$(KUBECTL) set env deployment/meridio-2-controller-manager-common-appnet -n $$NS MERIDIO_TEMPLATE_PATH=/templates-override; \
	$(KUBECTL) patch deployment meridio-2-controller-manager-common-appnet -n $$NS --type=json -p='[{"op":"add","path":"/spec/template/spec/containers/0/volumeMounts/-","value":{"name":"templates-override","mountPath":"/templates-override"}},{"op":"add","path":"/spec/template/spec/volumes/-","value":{"name":"templates-override","configMap":{"name":"lb-template-override-common-appnet"}}}]'; \
	$(KUBECTL) wait --for=condition=Available --timeout=120s -n $$NS deployment/meridio-2-controller-manager-common-appnet; \
	$(KUBECTL) wait --for=condition=Ready --timeout=60s -n $$NS certificate/meridio-2-serving-cert-common-appnet; \
	$(KUBECTL) apply -f $$SUITE_DIR/rbac.yaml; \
	$(KUBECTL) apply -f $$SUITE_DIR/nad.yaml -n $$NS; \
	$(KUBECTL) apply -f $$SUITE_DIR/gateway.yaml -n $$NS; \
	$(KUBECTL) apply -f $$SUITE_DIR/dg.yaml -n $$NS; \
	$(KUBECTL) apply -f $$SUITE_DIR/routing.yaml -n $$NS; \
	$(KUBECTL) wait --for=condition=Available --timeout=120s -n $$NS deployment/sllb-gw-b1 deployment/sllb-gw-b2; \
	$(KUBECTL) apply -f $$SUITE_DIR/targets.yaml -n $$NS; \
	$(KUBECTL) wait --for=condition=Available --timeout=120s -n $$NS deployment/target-b

.PHONY: test-common-appnetwork
test-common-appnetwork: ## Run e2e tests for common-appnetwork suite.
	cd $(PROJECT_ROOT)/test/e2e && go run github.com/onsi/ginkgo/v2/ginkgo -v --tags=e2e --focus="Common App Network"

.PHONY: undeploy-common-appnetwork
undeploy-common-appnetwork: ## Undeploy common-appnetwork topology.
	@NS=e2e-common-appnet; \
	$(KUBECTL) delete validatingwebhookconfiguration meridio-2-validating-webhook-configuration-common-appnet --ignore-not-found=true; \
	$(KUBECTL) delete namespace $$NS --ignore-not-found=true

################################################################################
##@ Suite: separate-appnetwork
################################################################################

.PHONY: separate-appnetwork
separate-appnetwork: deploy-separate-appnetwork test-separate-appnetwork ## Deploy and test separate-appnetwork suite.

.PHONY: deploy-separate-appnetwork
deploy-separate-appnetwork: cluster ## Deploy separate-appnetwork topology.
	@NS=e2e-separate-appnet; \
	SUITE_DIR=$(shell pwd)/suites/separate-appnetwork; \
	$(KUBECTL) create namespace $$NS --dry-run=client -o yaml | $(KUBECTL) apply -f -; \
	$(KUBECTL) label namespace $$NS e2e-suite=separate-appnet --overwrite; \
	$(KUBECTL) apply -f suites/common/gatewayclass.yaml; \
	sed 's|registry.nordix.org/cloud-native/meridio-2|$(REGISTRY)|g; s|:latest|:$(VERSION)|g' \
		$(PROJECT_ROOT)/config/templates/lb-deployment.yaml | \
		$(KUBECTL) create configmap lb-template-override-separate-appnet --from-file=lb-deployment.yaml=/dev/stdin -n $$NS --dry-run=client -o yaml | \
		$(KUBECTL) apply -f -; \
	cd $$SUITE_DIR && $(KUSTOMIZE) edit set image controller-manager=$(REGISTRY)/controller-manager:$(VERSION); \
	cd $$SUITE_DIR && $(KUSTOMIZE) build . | $(KUBECTL) apply -f -; \
	$(KUBECTL) set env deployment/meridio-2-controller-manager-separate-appnet -n $$NS MERIDIO_TEMPLATE_PATH=/templates-override; \
	$(KUBECTL) patch deployment meridio-2-controller-manager-separate-appnet -n $$NS --type=json -p='[{"op":"add","path":"/spec/template/spec/containers/0/volumeMounts/-","value":{"name":"templates-override","mountPath":"/templates-override"}},{"op":"add","path":"/spec/template/spec/volumes/-","value":{"name":"templates-override","configMap":{"name":"lb-template-override-separate-appnet"}}}]'; \
	$(KUBECTL) wait --for=condition=Available --timeout=120s -n $$NS deployment/meridio-2-controller-manager-separate-appnet; \
	$(KUBECTL) wait --for=condition=Ready --timeout=60s -n $$NS certificate/meridio-2-serving-cert-separate-appnet; \
	$(KUBECTL) apply -f $$SUITE_DIR/rbac.yaml; \
	$(KUBECTL) apply -f $$SUITE_DIR/nad.yaml -n $$NS; \
	$(KUBECTL) apply -f $$SUITE_DIR/gateway.yaml -n $$NS; \
	$(KUBECTL) apply -f $$SUITE_DIR/dg.yaml -n $$NS; \
	$(KUBECTL) apply -f $$SUITE_DIR/routing.yaml -n $$NS; \
	$(KUBECTL) wait --for=condition=Available --timeout=120s -n $$NS deployment/sllb-gw-a1 deployment/sllb-gw-a2; \
	$(KUBECTL) apply -f $$SUITE_DIR/targets.yaml -n $$NS; \
	$(KUBECTL) wait --for=condition=Available --timeout=120s -n $$NS deployment/target-a

.PHONY: test-separate-appnetwork
test-separate-appnetwork: ## Run e2e tests for separate-appnetwork suite.
	cd $(PROJECT_ROOT)/test/e2e && go run github.com/onsi/ginkgo/v2/ginkgo -v --tags=e2e --focus="Separate App Network"

.PHONY: undeploy-separate-appnetwork
undeploy-separate-appnetwork: ## Undeploy separate-appnetwork topology.
	@NS=e2e-separate-appnet; \
	$(KUBECTL) delete validatingwebhookconfiguration meridio-2-validating-webhook-configuration-separate-appnet --ignore-not-found=true; \
	$(KUBECTL) delete namespace $$NS --ignore-not-found=true

################################################################################
##@ All Suites
################################################################################

.PHONY: deploy-all
deploy-all: ## Deploy all suites.
	@for suite in $(SUITES); do $(MAKE) deploy-$$suite || exit 1; done

.PHONY: test
test: deploy-all ## Run all e2e tests in parallel.
	cd $(PROJECT_ROOT)/test/e2e && go run github.com/onsi/ginkgo/v2/ginkgo -v -p --tags=e2e

.PHONY: undeploy-all
undeploy-all: ## Undeploy all suites.
	@for suite in $(SUITES); do $(MAKE) undeploy-$$suite; done
