# Credential refresher: every login-capable agent CLI plus kubectl and the
# publish scripts. It holds the master logins on a persistent home, keeps their
# short-lived access tokens fresh, and publishes sandbox-safe copies. Build
# from the repository root:
#   docker build -f images/creds/Dockerfile -t nightshift-creds:local .
# Build images/base first, or pass a published base with --build-arg BASE_IMAGE.
ARG BASE_IMAGE=nightshift-base:local
FROM ${BASE_IMAGE}

ARG TARGETARCH
USER root

# The agents whose logins are refreshed here. Each install verifies a checksum
# where the vendor publishes one; the npm packages run their install scripts.
RUN npm install --global @anthropic-ai/claude-code @openai/codex \
    && npm cache clean --force

RUN case "${TARGETARCH}" in amd64) platform=linux_amd64 ;; arm64) platform=linux_arm64 ;; *) exit 1 ;; esac \
    && manifest=$(curl -fsSL "https://antigravity-cli-auto-updater-974169037036.us-central1.run.app/manifests/${platform}.json") \
    && url=$(echo "${manifest}" | jq -er .url) \
    && sha512=$(echo "${manifest}" | jq -er .sha512) \
    && curl -fsSL "${url}" -o /tmp/agy.tar.gz \
    && echo "${sha512}  /tmp/agy.tar.gz" | sha512sum -c - \
    && tar -xzf /tmp/agy.tar.gz -C /usr/local/bin antigravity \
    && mv /usr/local/bin/antigravity /usr/local/bin/agy \
    && rm /tmp/agy.tar.gz

RUN version=$(curl -fsSL https://dl.k8s.io/release/stable.txt) \
    && curl -fsSLo /usr/local/bin/kubectl "https://dl.k8s.io/release/${version}/bin/linux/${TARGETARCH}/kubectl" \
    && echo "$(curl -fsSL "https://dl.k8s.io/release/${version}/bin/linux/${TARGETARCH}/kubectl.sha256")  /usr/local/bin/kubectl" | sha256sum -c - \
    && chmod +x /usr/local/bin/kubectl

COPY k8s/creds-publish.sh k8s/creds-stage.sh k8s/creds-providers.json /usr/local/share/nightshift/
RUN chmod +x /usr/local/share/nightshift/*.sh \
    && ln -s /usr/local/share/nightshift/creds-publish.sh /usr/local/bin/creds-publish

USER node
WORKDIR /home/node
ENTRYPOINT ["creds-publish"]
CMD ["--watch", "1200"]
