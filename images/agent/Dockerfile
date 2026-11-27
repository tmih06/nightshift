# Build images/base first, or pass a published base with --build-arg BASE_IMAGE.
ARG BASE_IMAGE=nightshift-base:local
FROM ${BASE_IMAGE}

ARG AGENT=pi
ARG AGENT_VERSION=1.5.2
ARG TARGETARCH
USER root

# agent.sh installs the agent its agents/<AGENT> directory describes: an npm
# package, whose install scripts select the platform binary, or the agent's
# own install.sh, which verifies the published checksum before the binary
# lands in PATH. It checks the version the CLI reports and stamps it into
# /etc/nightshift-agent.
COPY agent.sh /tmp/nightshift-agent/agent.sh
COPY agents/ /tmp/nightshift-agent/agents/
RUN bash /tmp/nightshift-agent/agent.sh install "${AGENT}" "${AGENT_VERSION}" \
    && rm -rf /tmp/nightshift-agent

USER node
