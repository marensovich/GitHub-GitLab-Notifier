# syntax=docker/dockerfile:1

# ─── Build stage ───
# Runs on the build machine's platform and cross-compiles for the target one,
# which is much faster than building under QEMU emulation.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=docker
ARG TARGETOS TARGETARCH TARGETVARIANT
RUN GOARM="${TARGETVARIANT#v}" \
    CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
    go build -trimpath \
      -ldflags "-s -w -X main.Version=${VERSION} -X main.BuildTime=$(date -u '+%Y-%m-%d_%H:%M:%S')" \
      -o /out/notifier . \
 && mkdir -p /out/data

# ─── Runtime stage ───
# Static binary (pure Go SQLite), CA certificates and tzdata only.
FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.title="GitHub-GitLab-Notifier" \
      org.opencontainers.image.description="Telegram notifications for GitHub and GitLab webhooks" \
      org.opencontainers.image.source="https://github.com/marensovich/GitHub-GitLab-Notifier" \
      org.opencontainers.image.licenses="MIT"

COPY --from=build /out/notifier /usr/local/bin/notifier
COPY --from=build --chown=nonroot:nonroot /out/data /data

# Used only when no config.yaml is mounted (1.x settings in the environment).
ENV DB_PATH=/data/notifier.db \
    SERVER_ADDRESS=0.0.0.0 \
    SERVER_PORT=8080

# From /, the default storage.path data/notifier.db is on the volume.
# Mount the configuration at /etc/notifier/config.yaml.
WORKDIR /
VOLUME ["/data"]
EXPOSE 8080
USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/notifier"]
CMD ["serve"]
