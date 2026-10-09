# syntax=docker/dockerfile:1

# Build stage: pinned toolchain, dependency-cached, static binary.
FROM golang:1.27.2-alpine3.23 AS build

WORKDIR /src

# Download dependencies first so source edits do not invalidate the layer.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO is disabled so the result is a static binary that runs on a scratch-like
# base without a libc dependency.
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/api ./cmd/api

# Runtime stage: the binary, a CA bundle, and a non-root user. Nothing else.
FROM alpine:3.23

# Certificates are needed if the service later calls out over HTTPS. The tzdata
# package keeps container time zones predictable.
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 app \
    && adduser -S -u 10001 -G app app

COPY --from=build /out/api /usr/local/bin/api

USER app:app

EXPOSE 8080

# The binary is PID 1 and traps SIGINT/SIGTERM, so `docker stop` triggers a
# graceful shutdown rather than an immediate kill.
STOPSIGNAL SIGTERM

ENTRYPOINT ["/usr/local/bin/api"]