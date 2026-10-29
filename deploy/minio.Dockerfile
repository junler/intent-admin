# syntax=docker/dockerfile:1.7
# Build official MinIO source reproducibly, without relying on mutable registry
# images. MinIO is distributed under AGPL-3.0; see the included upstream license.
FROM golang:1.26-bookworm AS build
ARG MINIO_COMMIT=07c3a429bfed433e49018cb0f78a52145d4bedeb
ARG CONSOLE_COMMIT=2017f33b26e1cb632dd208ab7d91add1d06990fd
WORKDIR /src/minio
RUN --mount=type=secret,id=proxy_ca \
    if [ -f /run/secrets/proxy_ca ]; then cat /etc/ssl/certs/ca-certificates.crt /run/secrets/proxy_ca > /tmp/intent-build-ca.crt; export SSL_CERT_FILE=/tmp/intent-build-ca.crt GIT_SSL_CAINFO=/tmp/intent-build-ca.crt; fi; \
    git init . && git remote add origin https://github.com/minio/minio.git \
    && git fetch --depth 1 origin "$MINIO_COMMIT" && git checkout --detach FETCH_HEAD \
    && git init /src/console && git -C /src/console remote add origin https://github.com/chainguard-forks/minio-console.git \
    && git -C /src/console fetch --depth 1 origin "$CONSOLE_COMMIT" && git -C /src/console checkout --detach FETCH_HEAD \
    && go mod edit -replace github.com/minio/console=/src/console \
    && CGO_ENABLED=0 GOTOOLCHAIN=local go build -buildvcs=false -p 2 -trimpath -o /out/minio . \
    && mkdir -p /out/data && rm -f /tmp/intent-build-ca.crt
FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.source="https://github.com/minio/minio" \
      org.opencontainers.image.revision="07c3a429bfed433e49018cb0f78a52145d4bedeb" \
      org.opencontainers.image.licenses="AGPL-3.0-only"
COPY --from=build --chmod=755 /out/minio /usr/local/bin/minio
COPY --from=build /src/minio/LICENSE /licenses/minio/LICENSE
COPY --from=build /src/console/LICENSE /licenses/minio-console/LICENSE
COPY --from=build --chown=65532:65532 /out/data /data
USER 65532:65532
EXPOSE 9000 9001
ENTRYPOINT ["/usr/local/bin/minio"]
CMD ["server", "/data", "--console-address", ":9001"]
