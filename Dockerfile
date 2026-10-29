# syntax=docker/dockerfile:1.7
FROM node:22-bookworm-slim AS frontend
WORKDIR /src/web
COPY web/ ./
# An optional session CA supports managed build proxies without weakening TLS.
RUN --mount=type=secret,id=proxy_ca \
    if [ -f /run/secrets/proxy_ca ]; then export NODE_EXTRA_CA_CERTS=/run/secrets/proxy_ca; fi; \
    npm ci --strict-ssl=true
RUN npm run build

FROM golang:1.26-bookworm AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=secret,id=proxy_ca \
    if [ -f /run/secrets/proxy_ca ]; then cat /etc/ssl/certs/ca-certificates.crt /run/secrets/proxy_ca > /tmp/intent-build-ca.crt; export SSL_CERT_FILE=/tmp/intent-build-ca.crt; fi; \
    go mod download && rm -f /tmp/intent-build-ca.crt
COPY . ./
COPY --from=frontend /src/web/dist/ ./internal/web/dist/
ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_TIME=unknown
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/junler/intent-admin/internal/bootstrap.Version=${VERSION} -X github.com/junler/intent-admin/internal/bootstrap.Commit=${COMMIT} -X github.com/junler/intent-admin/internal/bootstrap.BuildTime=${BUILD_TIME}" -o /out/intent-admin . \
    && CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o /out/healthcheck ./cmd/healthcheck \
    && CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o /out/storage-init ./cmd/storage-init \
    && mkdir -p /out/uploads

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=backend --chmod=755 /out/intent-admin /usr/local/bin/intent-admin
COPY --from=backend --chmod=755 /out/healthcheck /usr/local/bin/healthcheck
COPY --from=backend --chmod=755 /out/storage-init /usr/local/bin/storage-init
COPY --from=backend /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=backend --chown=65532:65532 /out/uploads /var/lib/intent/uploads
ENV INTENT_ADMIN_APP_PORT=8080 \
    INTENT_ADMIN_STORAGE_BOOTSTRAP_LOCAL_DIR=/var/lib/intent/uploads
USER 65532:65532
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=15s --retries=5 CMD ["/usr/local/bin/healthcheck"]
ENTRYPOINT ["/usr/local/bin/intent-admin"]
CMD ["serve"]
