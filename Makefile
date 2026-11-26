.PHONY: lint fmt fmt-check gate gate-full quality-tools quality-baseline

GO_VERSION := $(shell sed -n 's/^go //p' go.mod)
TOOLS_BIN := $(CURDIR)/.tools/bin

# Kubernetes YAML omits document markers and aligns sequences with their keys.
# Markdown diagrams and tables need no line-length or table-alignment rules.
lint:
	shellcheck k8s/sbx.sh k8s/egress-setup.sh scripts/quality/gate scripts/quality/stop-hook scripts/quality/bench-compare scripts/quality/diff-check/run
	pipx run yamllint -d '{extends: default, rules: {document-start: disable, indentation: {indent-sequences: false}}}' k8s/
	@tmp=$$(mktemp -d); trap 'rm -rf "$$tmp"' EXIT; \
		printf '%s\n' '{"config":{"default":true,"MD013":false,"MD060":false}}' > "$$tmp/config.jsonc"; \
		npx --yes markdownlint-cli2 --config "$$tmp/config.jsonc" "*.md" "docs/*.md"

fmt:
	gofmt -w .

fmt-check:
	test -z "$$(gofmt -l .)"

# Fast local checks: build, lint on changed packages, and the diff rules.
gate:
	@GATE_PROFILE=fast scripts/quality/gate

# Every check, as CI runs it. The race tests take about ten minutes.
gate-full:
	@GATE_PROFILE=full scripts/quality/gate

# Installs the gate's pinned tools into .tools/bin, built with the module's Go.
quality-tools:
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
