# syntax=docker/dockerfile:1

# ---- Frontend build ---------------------------------------------------------
# The Svelte/TypeScript single-page applications are compiled to static assets
# and then embedded into the Go binary, so the runtime image ships one file.
FROM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# ---- Backend build ----------------------------------------------------------
FROM golang:1.24-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Embed the freshly built frontend so the binary is self-contained.
COPY --from=web /web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o /out/texshare .

# ---- Runtime ----------------------------------------------------------------
FROM alpine:3.20 AS runtime

# ClamAV provides the managed antivirus engine (clamd, freshclam, clamdscan).
# tzdata keeps timestamps sensible; su-exec lets the entrypoint drop privileges.
RUN apk add --no-cache ca-certificates tzdata clamav su-exec wget \
    && addgroup -g 1000 texshare \
    && adduser -D -u 1000 -G texshare texshare \
    && mkdir -p /data \
    && chown -R texshare:texshare /data

# Optional Cloudflare Tunnel binary for public hostname provisioning.
ARG CLOUDFLARED_VERSION=2024.12.2
RUN arch="$(apk --print-arch)" \
    && case "$arch" in \
         x86_64) cfarch=amd64 ;; \
         aarch64) cfarch=arm64 ;; \
         *) cfarch=amd64 ;; \
       esac \
    && wget -q -O /usr/local/bin/cloudflared \
         "https://github.com/cloudflare/cloudflared/releases/download/${CLOUDFLARED_VERSION}/cloudflared-linux-${cfarch}" \
    && chmod +x /usr/local/bin/cloudflared \
    && /usr/local/bin/cloudflared --version

COPY --from=build /out/texshare /usr/local/bin/texshare
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

ENV TEXSHARE_DATA_DIR=/data \
    TEXSHARE_PUBLIC_ADDR=0.0.0.0:3535 \
    TEXSHARE_ADMIN_ADDR=0.0.0.0:3636

VOLUME ["/data"]
EXPOSE 3535 3636

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=5 \
    CMD wget -qO- http://127.0.0.1:3535/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
