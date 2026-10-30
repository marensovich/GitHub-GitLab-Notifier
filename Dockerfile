# syntax=docker/dockerfile:1

# ─── Build stage ───
FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=docker
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X main.Version=${VERSION} -X main.BuildTime=$(date -u '+%Y-%m-%d_%H:%M:%S')" \
      -o /out/notifier . \
 && mkdir -p /out/data

# ─── Runtime stage ───
# Static binary (pure Go SQLite), CA certificates and tzdata only.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/notifier /usr/local/bin/notifier
COPY --from=build --chown=nonroot:nonroot /out/data /data

ENV DB_PATH=/data/notifier.db \
    SERVER_ADDRESS=0.0.0.0 \
    SERVER_PORT=8080

WORKDIR /data
VOLUME ["/data"]
EXPOSE 8080
USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/notifier"]
