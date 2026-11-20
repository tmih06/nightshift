# Build images/base first, or pass a published base with --build-arg BASE_IMAGE.
ARG BASE_IMAGE=nightshift-base:local
FROM ${BASE_IMAGE}

ARG AGENT=pi
ARG AGENT_VERSION=1.5.2
USER root

# Some agents need npm's install scripts to select their platform binary.
RUN case "${AGENT}" in \
      pi) package=@earendil-works/pi-coding-agent; command=pi ;; \
      codex) package=@openai/codex; command=codex ;; \
      claude-code) package=@anthropic-ai/claude-code; command=claude ;; \
      opencode) package=opencode-ai; command=opencode ;; \
      *) echo "unsupported agent: ${AGENT}" >&2; exit 1 ;; \
    esac \
    && echo "${AGENT_VERSION}" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' \
    && npm install --global "${package}@${AGENT_VERSION}" \
    && npm cache clean --force \
    && "${command}" --version | grep -F "${AGENT_VERSION}"

USER node
