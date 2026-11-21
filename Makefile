.PHONY: lint fmt fmt-check

# Kubernetes YAML omits document markers and aligns sequences with their keys.
# Markdown diagrams and tables need no line-length or table-alignment rules.
lint:
	shellcheck k8s/sbx.sh
	pipx run yamllint -d '{extends: default, rules: {document-start: disable, indentation: {indent-sequences: false}}}' k8s/
	@tmp=$$(mktemp -d); trap 'rm -rf "$$tmp"' EXIT; \
		printf '%s\n' '{"config":{"default":true,"MD013":false,"MD060":false}}' > "$$tmp/config.jsonc"; \
		npx --yes markdownlint-cli2 --config "$$tmp/config.jsonc" "*.md" "docs/*.md"

fmt:
	gofmt -w .

fmt-check:
	test -z "$$(gofmt -l .)"
