# Lightweight static egress proxy image (~15 MB, ~20-30 MiB RAM at runtime).
FROM golang:1.27-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/egress-proxy ./cmd/egress-proxy
COPY internal/egress ./internal/egress

ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/egress-proxy ./cmd/egress-proxy

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/egress-proxy /usr/local/bin/egress-proxy
USER 65532:65532
EXPOSE 8080 9090
ENTRYPOINT ["/usr/local/bin/egress-proxy"]
CMD ["-listen", ":8080", "-admin", ":9090", "-config", "/etc/egress/policy.yaml"]
