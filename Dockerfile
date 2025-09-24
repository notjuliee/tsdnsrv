# syntax=docker/dockerfile:1

ARG GO_VERSION=1.25.1

FROM golang:${GO_VERSION}-bookworm AS builder

WORKDIR /src

# Enable module download caching across builds
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ENV GOOS=$TARGETOS GOARCH=$TARGETARCH CGO_ENABLED=0

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags='-s -w' -o /out/tsdnsrv ./main.go

FROM gcr.io/distroless/base-debian12:latest

LABEL org.opencontainers.image.title="tsdnsrv" \
      org.opencontainers.image.description="Tailnet-backed DNS server"

COPY --from=builder /out/tsdnsrv /usr/local/bin/tsdnsrv

WORKDIR /app

EXPOSE 53/tcp
EXPOSE 53/udp

ENTRYPOINT ["/usr/local/bin/tsdnsrv"]
CMD ["--config=/config/server.json"]
