# Credential refresher: kubectl, jq, curl, and the publish scripts. It reads
# the accounts signed in to CLIProxyAPI (k8s/cpa.yaml) and publishes sandbox
# copies and real access tokens. Build from the repository root:
#   docker build -f images/creds/Dockerfile -t nightshift-creds:local .
# Build images/base first, or pass a published base with --build-arg BASE_IMAGE.
ARG BASE_IMAGE=nightshift-base:local
FROM ${BASE_IMAGE}

ARG TARGETARCH
USER root

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
CMD ["--cpa", "http://cpa.nightshift.svc:8317", "--watch", "300"]
