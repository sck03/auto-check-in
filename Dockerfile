FROM --platform=$BUILDPLATFORM node:24.18.0-bookworm-slim AS frontend
WORKDIR /source/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27.2-bookworm AS builder
WORKDIR /source
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /source/web/dist ./web/dist
COPY --from=frontend /source/web/node_modules ./web/node_modules
COPY --from=frontend /usr/local/bin/node /usr/local/bin/node
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=1.0.1
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X auto-check-in/internal/buildinfo.Version=$VERSION" -o /out/checkin-server ./cmd/server
RUN node scripts/licenses.mjs --out /source/dist/THIRD_PARTY_NOTICES.md

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    chromium ca-certificates tzdata fonts-noto-cjk tini \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 checkin \
    && useradd --uid 10001 --gid 10001 --create-home checkin \
    && mkdir -p /data && chown 10001:10001 /data
COPY --from=builder /out/checkin-server /usr/local/bin/checkin-server
COPY --from=builder /source/dist/THIRD_PARTY_NOTICES.md /usr/share/doc/auto-check-in/THIRD_PARTY_NOTICES.md
ENV CHECKIN_LISTEN=0.0.0.0:18990 \
    CHECKIN_DATA_DIR=/data \
    CHECKIN_BROWSER_PATH=/usr/bin/chromium \
    CHECKIN_BROWSER_NO_SANDBOX=true \
    XDG_CACHE_HOME=/tmp/cache \
    XDG_CONFIG_HOME=/tmp/config \
    TZ=Asia/Shanghai
USER 10001:10001
WORKDIR /data
EXPOSE 18990
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
    CMD ["checkin-server", "-healthcheck"]
ENTRYPOINT ["/usr/bin/tini", "--", "checkin-server"]
