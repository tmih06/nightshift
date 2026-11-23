# Build images/base first, or pass a published base with --build-arg BASE_IMAGE.
ARG BASE_IMAGE=nightshift-base:local
FROM ${BASE_IMAGE}

ARG AGENT=pi
ARG AGENT_VERSION=1.5.2
ARG TARGETARCH
USER root

# Most agents install from npm, where install scripts select the platform
# binary. agy and ompig-slim ship their own release downloads instead; each
# install verifies the published checksum before the binary lands in PATH.
RUN case "${AGENT}" in \
      pi) package=@earendil-works/pi-coding-agent; command=pi ;; \
      codex) package=@openai/codex; command=codex ;; \
      claude-code) package=@anthropic-ai/claude-code; command=claude ;; \
      opencode) package=opencode-ai; command=opencode ;; \
      agy) package=; command=agy ;; \
      ompig-slim) package=; command=ompig-slim ;; \
      *) echo "unsupported agent: ${AGENT}" >&2; exit 1 ;; \
    esac \
    && echo "${AGENT_VERSION}" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' \
    && if [ -n "${package}" ]; then \
         npm install --global "${package}@${AGENT_VERSION}" \
         && npm cache clean --force; \
       elif [ "${AGENT}" = agy ]; then \
         case "${TARGETARCH}" in amd64) platform=linux_amd64 ;; arm64) platform=linux_arm64 ;; *) exit 1 ;; esac \
         && manifest=$(curl -fsSL "https://antigravity-cli-auto-updater-974169037036.us-central1.run.app/manifests/${platform}.json") \
         && url=$(echo "${manifest}" | jq -er .url) \
         && sha512=$(echo "${manifest}" | jq -er .sha512) \
         && [ "$(echo "${manifest}" | jq -er .version)" = "${AGENT_VERSION}" ] \
         && curl -fsSL "${url}" -o /tmp/agy.tar.gz \
         && echo "${sha512}  /tmp/agy.tar.gz" | sha512sum -c - \
         && tar -xzf /tmp/agy.tar.gz -C /usr/local/bin antigravity \
         && mv /usr/local/bin/antigravity /usr/local/bin/agy \
         && chmod +x /usr/local/bin/agy \
         && rm /tmp/agy.tar.gz; \
       elif [ "${AGENT}" = ompig-slim ]; then \
         curl -fsSL https://raw.githubusercontent.com/tmih06/ompig-slim/main/scripts/install.sh \
           | OMPIG_SLIM_INSTALL_DIR=/usr/local/bin bash -s -- --version "v${AGENT_VERSION}" --dir /usr/local/bin; \
       else \
         echo "no installer for agent: ${AGENT}" >&2; exit 1; \
       fi \
    && "${command}" --version | grep -F "${AGENT_VERSION}" \
    && echo "${AGENT} ${AGENT_VERSION}" > /etc/nightshift-agent

USER node
