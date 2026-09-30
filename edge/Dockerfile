# Compiles the TypeScript UI to the static/js directory the Go binary embeds.
FROM --platform=$BUILDPLATFORM node:24-alpine AS web

WORKDIR /build

COPY package.json package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY tsconfig.base.json ./
COPY edge/static/tsconfig.json ./edge/static/
COPY edge/static/ts ./edge/static/ts
RUN npx tsc -p edge/static

FROM golang:1.26-alpine AS builder

WORKDIR /build

COPY go.work ./
COPY shared/go.mod shared/go.sum ./shared/
COPY edge/go.mod edge/go.sum ./edge/
COPY central/go.mod ./central/go.mod
# Keep module downloads cached when Go or UI sources change.
WORKDIR /build/edge
RUN go mod download

WORKDIR /build
COPY shared ./shared
COPY edge ./edge
COPY --from=web /build/edge/static/js ./edge/static/js
WORKDIR /build/edge
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /3to1go-edge ./cmd/server

FROM alpine:3.21

RUN apk add --no-cache ca-certificates curl

ENV XDG_CONFIG_HOME=/config
ENV XDG_STATE_HOME=/data/state
ENV XDG_CACHE_HOME=/data/cache

WORKDIR /srv/3to1go-edge

COPY --from=builder /3to1go-edge /usr/local/bin/3to1go-edge
COPY edge/docker-entrypoint.sh /usr/local/bin/3to1go-edge-entrypoint
RUN sed -i 's/\r$//' /usr/local/bin/3to1go-edge-entrypoint \
    && chmod +x /usr/local/bin/3to1go-edge-entrypoint

EXPOSE 6556

ENTRYPOINT ["/usr/local/bin/3to1go-edge-entrypoint"]
CMD ["/usr/local/bin/3to1go-edge"]
