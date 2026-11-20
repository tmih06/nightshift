# A ready-to-use coding sandbox. Build for the same architecture as the kind node.
FROM node:24-trixie-slim

ARG TARGETARCH

# Debian calls fd "fdfind"; expose the name agents expect as well.
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
       bash ca-certificates curl fd-find git jq less openssh-client procps \
       python3 ripgrep vim-tiny \
    && ln -s /usr/bin/fdfind /usr/local/bin/fd \
    && rm -rf /var/lib/apt/lists/*

# Pin and verify Herdr's release independently for each supported architecture.
RUN case "${TARGETARCH}" in \
      amd64) arch=x86_64; sha=78424755c10a40dde38a0bdb55c20ac19706e9ae05ed68fc2acf1fcff0d941c1 ;; \
      arm64) arch=aarch64; sha=e844a2bfab48a226097679d076bc83042fe57fccef5387ef802e5b24e78b529a ;; \
      *) echo "unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
    && curl -fsSL -A 'nightshift-image-build' "https://releases.herdr.dev/core/0.1.21/herdr-core-linux-${arch}.tar.gz?src=install" -o /tmp/hc.tar.gz \
    && echo "${sha}  /tmp/hc.tar.gz" | sha256sum -c - \
    && tar -xzf /tmp/hc.tar.gz -C /usr/local/bin herdr-core \
    && ln -s herdr-core /usr/local/bin/hc \
    && rm /tmp/hc.tar.gz

RUN mkdir -p /home/node/workspace && chown node:node /home/node/workspace
USER node
WORKDIR /home/node/workspace
CMD ["sleep", "infinity"]
