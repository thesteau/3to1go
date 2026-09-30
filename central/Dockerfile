# Compiles the TypeScript UI to the static/js directory the Go binary embeds.
FROM node:24-alpine AS web

WORKDIR /build

COPY package.json package-lock.json tsconfig.base.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY central/static/tsconfig.json ./central/static/
COPY central/static/ts ./central/static/ts
RUN npx tsc -p central/static

FROM golang:1.26-alpine AS builder

WORKDIR /build

COPY go.work ./
COPY shared/go.mod ./shared/go.mod
COPY central/go.mod central/go.sum ./central/
COPY edge/go.mod ./edge/go.mod
COPY shared ./shared
COPY central ./central
COPY --from=web /build/central/static/js ./central/static/js
WORKDIR /build/central
RUN --mount=type=cache,target=/root/.cache/go-build go mod download

WORKDIR /build/central
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /3to1go-central ./cmd/server

FROM alpine:3.21

RUN apk add --no-cache ca-certificates curl

ENV XDG_CONFIG_HOME=/config

WORKDIR /srv/3to1go-central

COPY --from=builder /3to1go-central /usr/local/bin/3to1go-central
COPY central/docker-entrypoint.sh /usr/local/bin/3to1go-central-entrypoint
RUN sed -i 's/\r$//' /usr/local/bin/3to1go-central-entrypoint \
    && chmod +x /usr/local/bin/3to1go-central-entrypoint

EXPOSE 6555

ENTRYPOINT ["/usr/local/bin/3to1go-central-entrypoint"]
CMD ["/usr/local/bin/3to1go-central"]
