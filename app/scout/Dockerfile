# Compiles the TypeScript UI to the static/js directory the Go binary embeds.
FROM --platform=$BUILDPLATFORM node:26-alpine AS web

WORKDIR /build

COPY package.json package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY tsconfig.base.json ./
COPY shared/ts ./shared/ts
COPY scout/static/tsconfig.json ./scout/static/
COPY scout/static/ts ./scout/static/ts
RUN npx tsc -p scout/static

# Runs on the build machine and cross-compiles for each target platform. With
# CGO off, Go needs no emulation, which is far faster than building on QEMU.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS builder
ARG TARGETOS TARGETARCH TARGETVARIANT
# Tag and commit hash shown in Admin and the startup log.
ARG VERSION=
ARG REVISION=

WORKDIR /build

COPY go.work ./
COPY shared/go/go.mod shared/go/go.sum ./shared/go/
COPY scout/go.mod scout/go.sum ./scout/
COPY station/go.mod ./station/go.mod
# Keep module downloads cached when Go or UI sources change.
WORKDIR /build/scout
RUN go mod download

WORKDIR /build
COPY shared/go ./shared/go
COPY scout ./scout
COPY --from=web /build/scout/static/js ./scout/static/js
WORKDIR /build/scout
# TARGETVARIANT is v7 for 32-bit ARM, which Go spells GOARM=7.
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=${TARGETVARIANT#v} \
    go build -trimpath -o /3to1go-scout \
    -ldflags="-s -w -X github.com/3to1go/shared/buildinfo.Version=$VERSION -X github.com/3to1go/shared/buildinfo.Commit=$REVISION" \
    ./cmd/server

FROM alpine:3.24
ARG VERSION=
ARG REVISION=
LABEL org.opencontainers.image.version=$VERSION org.opencontainers.image.revision=$REVISION

RUN apk add --no-cache ca-certificates curl

ENV XDG_CONFIG_HOME=/config
ENV XDG_STATE_HOME=/data/state
ENV XDG_CACHE_HOME=/data/cache

WORKDIR /srv/3to1go-scout

COPY --from=builder /3to1go-scout /usr/local/bin/3to1go-scout
COPY scout/docker-entrypoint.sh /usr/local/bin/3to1go-scout-entrypoint
RUN sed -i 's/\r$//' /usr/local/bin/3to1go-scout-entrypoint \
    && chmod +x /usr/local/bin/3to1go-scout-entrypoint

EXPOSE 6556

ENTRYPOINT ["/usr/local/bin/3to1go-scout-entrypoint"]
CMD ["/usr/local/bin/3to1go-scout"]
