# nightshift-auth: a static binary on distroless, like the egress proxy.
# Build from the repository root:
#   docker build -f images/auth/Dockerfile -t nightshift-auth:local .
FROM golang:1.27-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/nightshift-auth ./cmd/nightshift-auth
COPY internal/services/auth ./internal/services/auth

ARG TARGETOS=linux
ARG TARGETARCH
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/nightshift-auth ./cmd/nightshift-auth

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/nightshift-auth /usr/local/bin/nightshift-auth
USER 65532:65532
EXPOSE 8318
ENTRYPOINT ["/usr/local/bin/nightshift-auth"]
CMD ["-listen", ":8318", "-data", "/data"]
