# The Nightshift services: nightshiftd, the API every frontend uses, and the
# kubectl it drives the cluster with. Build from the repository root:
#   docker build -f images/nightshiftd/Dockerfile -t nightshiftd:local .
# For several architectures, use docker buildx build --platform with the same
# file and context.
FROM golang:1.27-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
# The services import only the contract and each other, which
# internal/architecture_test.go keeps true, so this is all they need.
COPY api ./api
COPY cmd/nightshiftd ./cmd/nightshiftd
COPY internal/services ./internal/services

ARG TARGETOS=linux
ARG TARGETARCH
ARG VERSION=dev
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH:-$(go env GOARCH)} \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/nightshiftd ./cmd/nightshiftd

FROM debian:bookworm-slim AS kubectl
ARG TARGETARCH
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
RUN arch="${TARGETARCH:-$(dpkg --print-architecture)}" \
    && version=$(curl -fsSL https://dl.k8s.io/release/stable.txt) \
    && curl -fsSLo /usr/local/bin/kubectl "https://dl.k8s.io/release/${version}/bin/linux/${arch}/kubectl" \
    && echo "$(curl -fsSL "https://dl.k8s.io/release/${version}/bin/linux/${arch}/kubectl.sha256")  /usr/local/bin/kubectl" | sha256sum -c - \
    && chmod +x /usr/local/bin/kubectl

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=kubectl /usr/local/bin/kubectl /usr/local/bin/kubectl
COPY --from=build /out/nightshiftd /usr/local/bin/nightshiftd
USER 65532:65532
EXPOSE 7777
ENTRYPOINT ["/usr/local/bin/nightshiftd"]
