# syntax=docker/dockerfile:1.7
ARG GO_BUILDER=golang:1.27.3-alpine3.24
ARG XRAY_IMAGE=ghcr.io/xtls/xray-core:26.10.21

FROM --platform=$BUILDPLATFORM ${GO_BUILDER} AS build
WORKDIR /src
COPY go.mod ./
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN mkdir -p /out && CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/omidiran ./cmd/server

# The official Xray image is used only as a verified binary source.
# Its documented executable path is /usr/local/bin/xray.
FROM --platform=$TARGETPLATFORM ${XRAY_IMAGE} AS xray

FROM alpine:3.24.3
RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 10001 omid \
    && adduser -S -D -H -u 10001 -G omid omid
WORKDIR /app
COPY --from=build /out/omidiran /usr/local/bin/omidiran
COPY --from=xray /usr/local/bin/xray /usr/local/bin/xray
COPY --chown=10001:10001 configs ./configs
COPY --chown=10001:10001 web ./web
RUN mkdir -p /app/data && chown -R omid:omid /app
USER 10001:10001
ENV PORT=8080 \
    DATA_FILE=/app/data/omid_state.json \
    XRAY_CONFIG=/app/data/xray-generated.json \
    XRAY_BIN=/usr/local/bin/xray \
    XRAY_AUTOSTART=true \
    XRAY_AUTORESTART=true
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=4s --start-period=15s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/ready || exit 1
ENTRYPOINT ["/usr/local/bin/omidiran"]
