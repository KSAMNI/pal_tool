# syntax=docker/dockerfile:1.7

ARG NODE_VERSION=22-bookworm-slim
ARG GO_VERSION=1.26-bookworm

FROM node:${NODE_VERSION} AS web-build
WORKDIR /src

COPY web/package.json web/package-lock.json ./web/
WORKDIR /src/web
RUN npm ci

COPY web/ /src/web/
COPY internal/frontend/ /src/internal/frontend/
RUN npm run build

FROM golang:${GO_VERSION} AS go-build
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=docker
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY --from=web-build /src/internal/frontend/dist/ ./internal/frontend/dist/

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/palpanel ./cmd/palpanel

FROM debian:bookworm-slim AS runtime
ARG TARGETARCH=amd64

RUN if [ "${TARGETARCH}" != "amd64" ]; then \
        echo "PalPanel Lite Docker runtime currently supports linux/amd64 only because SteamCMD and Palworld Dedicated Server are x86_64 targets." >&2; \
        exit 1; \
    fi \
    && apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl gzip lib32gcc-s1 lib32stdc++6 tar tzdata \
    && rm -rf /var/lib/apt/lists/*

RUN useradd --system --uid 10001 --home-dir /data --no-create-home palpanel \
    && mkdir -p /data /palserver \
    && chown -R palpanel:palpanel /data /palserver

RUN mkdir -p /usr/local/share/steamcmd \
    && curl -fsSL --retry 3 https://steamcdn-a.akamaihd.net/client/installer/steamcmd_linux.tar.gz \
        -o /usr/local/share/steamcmd/steamcmd_linux.tar.gz \
    && printf '%s\n' \
        '#!/bin/sh' \
        'set -eu' \
        'STEAMCMD_DIR="${PALPANEL_STEAMCMD_DIR:-/data/steamcmd}"' \
        'mkdir -p "$STEAMCMD_DIR"' \
        'if [ ! -x "$STEAMCMD_DIR/steamcmd.sh" ]; then' \
        '    tar -xzf /usr/local/share/steamcmd/steamcmd_linux.tar.gz -C "$STEAMCMD_DIR"' \
        'fi' \
        'cd "$STEAMCMD_DIR"' \
        'exec "$STEAMCMD_DIR/steamcmd.sh" "$@"' \
        > /usr/local/bin/steamcmd \
    && chmod 0755 /usr/local/bin/steamcmd

COPY --from=go-build /out/palpanel /usr/local/bin/palpanel

ENV HOME=/data \
    PALPANEL_ADDR=0.0.0.0:8080 \
    PALPANEL_DATA_DIR=/data \
    PALPANEL_STEAMCMD_DIR=/data/steamcmd \
    PALPANEL_DEFAULT_PAL_SERVER_PATH=/palserver \
    PALPANEL_PAL_SERVER_PATH=/palserver \
    PALWORLD_SERVER_PATH=/palserver \
    PAL_SERVER_PATH=/palserver

USER palpanel
WORKDIR /data
EXPOSE 8080
VOLUME ["/data", "/palserver"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -fsS http://127.0.0.1:8080/api/health || exit 1

ENTRYPOINT ["/usr/local/bin/palpanel"]
