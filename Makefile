.PHONY: help up redeploy images warmpool tui once forward status logs build test \
	lint fmt fmt-check gate gate-full quality-tools quality-baseline

.DEFAULT_GOAL := help

# The cluster to act on, and the local port the API is forwarded to.
CONTEXT ?= $(or $(NIGHTSHIFT_CONTEXT),$(KUBE_CONTEXT),kind-agent-sandbox)
PORT ?= 7777
KIND_CLUSTER ?= $(patsubst kind-%,%,$(CONTEXT))
KUBECTL = kubectl --context $(CONTEXT)
WITH_API = NIGHTSHIFT_CONTEXT=$(CONTEXT) PORT=$(PORT) scripts/dev/with-api

GO_VERSION := $(shell sed -n 's/^go //p' go.mod)
TOOLS_BIN := $(CURDIR)/.tools/bin

help: ## List these commands
	@awk -F ':.*## ' '/^[a-z-]+:.*## / { printf "  make %-18s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
	@printf '\nVariables: CONTEXT=%s PORT=%s ARGS="-repos"\n' '$(CONTEXT)' '$(PORT)'

# The setup scripts copy any API keys in your environment into the egress proxy,
# start nightshift-auth, and the manifest resets warm pool sizes.
up: ## Install or update the whole stack (applies every manifest)
ifneq ($(filter kind-%,$(CONTEXT)),)
	docker build -t nightshift-base:local images/base
endif
	NIGHTSHIFT_CONTEXT=$(CONTEXT) k8s/egress-setup.sh
	NIGHTSHIFT_CONTEXT=$(CONTEXT) k8s/auth-setup.sh
	$(KUBECTL) apply -f k8s/warmpool.yaml
	NIGHTSHIFT_CONTEXT=$(CONTEXT) k8s/nightshiftd-setup.sh

images: # Builds the local service images and loads them into kind.
	@case '$(CONTEXT)' in kind-*) ;; *) echo 'redeploy only works on a kind context' >&2; exit 1 ;; esac
	docker build -t nightshift-base:local images/base
	docker build -t nightshift-egress-proxy:local -f images/egress/Dockerfile .
	docker build -t nightshift-auth:local -f images/auth/Dockerfile .
	kind load docker-image nightshift-egress-proxy:local nightshift-auth:local --name $(KIND_CLUSTER)

# Leaves secrets, replica counts, and warm pools as they are.
redeploy: images ## Rebuild from this checkout and restart (kind only)
	$(KUBECTL) apply -f k8s/egress.yaml
	$(KUBECTL) -n nightshift-egress rollout restart deploy/egress-proxy
	$(KUBECTL) -n nightshift-egress rollout status deploy/egress-proxy --timeout=120s
	NIGHTSHIFT_CONTEXT=$(CONTEXT) AUTH_IMAGE=nightshift-auth:local k8s/auth-setup.sh
	$(KUBECTL) -n nightshift rollout restart deploy/nightshift-auth
	NIGHTSHIFT_CONTEXT=$(CONTEXT) k8s/nightshiftd-setup.sh

# One template and pool per directory under images/agent/agents.
warmpool: ## Render k8s/warmpool.yaml from the agents
	k8s/warmpool.sh > k8s/warmpool.yaml

tui: ## Open the dashboard (forwards the API for you)
	@$(WITH_API) go run ./cmd/nightshift $(ARGS)

once: ## Print one frame of the dashboard
	@$(WITH_API) go run ./cmd/nightshift -once $(ARGS)

forward: ## Keep the API forwarded, for another frontend
	$(KUBECTL) -n nightshift port-forward svc/nightshiftd $(PORT):7777

status: ## Show pods, warm pools, and claims
	@$(KUBECTL) -n nightshift get pods,sandboxwarmpool,sandboxclaim
	@$(KUBECTL) -n nightshift-egress get pods

logs: ## Follow the services' log
	$(KUBECTL) -n nightshift logs -f deploy/nightshiftd --tail=100

build: ## Compile every package
	go build ./...

test: ## Run the Go tests
	go test ./...

# Kubernetes YAML omits document markers and aligns sequences with their keys.
# Markdown diagrams and tables need no line-length or table-alignment rules.
lint: ## Lint scripts, manifests, and docs
	shellcheck k8s/sbx.sh k8s/egress-setup.sh k8s/auth-setup.sh k8s/warmpool.sh images/agent/agent.sh images/agent/agents/*/*.sh scripts/dev/with-api scripts/quality/gate scripts/quality/stop-hook scripts/quality/bench-compare scripts/quality/diff-check/run
	pipx run yamllint -d '{extends: default, rules: {document-start: disable, indentation: {indent-sequences: false}}}' k8s/
	@tmp=$$(mktemp -d); trap 'rm -rf "$$tmp"' EXIT; \
		printf '%s\n' '{"config":{"default":true,"MD013":false,"MD060":false}}' > "$$tmp/config.jsonc"; \
		npx --yes markdownlint-cli2 --config "$$tmp/config.jsonc" "*.md" "docs/*.md"

fmt: ## Format Go files
	gofmt -w .

fmt-check:
	test -z "$$(gofmt -l .)"

# Fast local checks: build, lint on changed packages, and the diff rules.
gate: ## Run the fast quality gate
	@GATE_PROFILE=fast scripts/quality/gate

# Every check, as CI runs it. The race tests take about ten minutes.
gate-full: ## Run every check CI runs (about ten minutes)
	@GATE_PROFILE=full scripts/quality/gate

# Installs the gate's pinned tools into .tools/bin, built with the module's Go.
quality-tools: ## Install the gate's tools (once)
	@mkdir -p "$(TOOLS_BIN)"
	@for pkg in \
		github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 \
		golang.org/x/vuln/cmd/govulncheck@v1.10.0 \
		golang.org/x/tools/cmd/deadcode@v0.53.0 \
		golang.org/x/perf/cmd/benchstat@v0.0.0-20261117185222-4febdb082b2c \
		github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0; do \
		GOTOOLCHAIN=go$(GO_VERSION) GOBIN="$(TOOLS_BIN)" go install "$$pkg" || exit 1; \
	done

# Locks in debt that has fallen. It can only lower the recorded counts.
quality-baseline:
	@scripts/quality/gate collect
	@go run ./scripts/quality/ratchet update
