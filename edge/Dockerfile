# Compiles the TypeScript UI to the static/js directory the Go binary embeds.
FROM --platform=$BUILDPLATFORM node:26-alpine AS web

WORKDIR /build

COPY package.json package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY tsconfig.base.json ./
COPY shared/ts ./shared/ts
COPY edge/static/tsconfig.json ./edge/static/
COPY edge/static/ts ./edge/static/ts
RUN npx tsc -p edge/static

# Runs on the build machine and cross-compiles for each target platform. With
# CGO off, Go needs no emulation, which is far faster than building on QEMU.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS builder
ARG TARGETOS TARGETARCH TARGETVARIANT

WORKDIR /build

COPY go.work ./
COPY shared/go/go.mod shared/go/go.sum ./shared/go/
COPY edge/go.mod edge/go.sum ./edge/
COPY central/go.mod ./central/go.mod
# Keep module downloads cached when Go or UI sources change.
WORKDIR /build/edge
RUN go mod download

WORKDIR /build
COPY shared/go ./shared/go
COPY edge ./edge
COPY --from=web /build/edge/static/js ./edge/static/js
WORKDIR /build/edge
# TARGETVARIANT is v7 for 32-bit ARM, which Go spells GOARM=7.
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=${TARGETVARIANT#v} \
    go build -trimpath -ldflags="-s -w" -o /3to1go-edge ./cmd/server

FROM alpine:3.24

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
