# syntax=docker/dockerfile:1

# --- Build: static binary (pure Go SQLite driver, no CGO) ---
FROM golang:1.23-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags '-s -w' -o /out/benevole ./cmd/benevole \
 && mkdir -p /out/data

# --- Run: distroless, non-root (uid 65532), CA certs and tzdata included ---
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/benevole /usr/local/bin/benevole
# Empty data dir owned by the runtime user, so a named volume mounted here
# is writable out of the box.
COPY --from=build --chown=65532:65532 /out/data /data

ENV BENEVOLE_ADDR=:8080 \
    BENEVOLE_DB=/data/benevole.db \
    TZ=Europe/Paris
VOLUME ["/data"]
EXPOSE 8080
USER nonroot:nonroot

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD ["/usr/local/bin/benevole", "-healthcheck"]

ENTRYPOINT ["/usr/local/bin/benevole"]
