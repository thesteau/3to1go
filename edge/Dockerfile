FROM golang:1.26-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /3to1go-edge ./cmd/server

FROM alpine:3.21

RUN apk add --no-cache ca-certificates curl

ENV XDG_CONFIG_HOME=/config
ENV XDG_STATE_HOME=/data/state
ENV XDG_CACHE_HOME=/data/cache

WORKDIR /srv/3to1go-edge

COPY --from=builder /3to1go-edge /usr/local/bin/3to1go-edge
COPY docker-entrypoint.sh /usr/local/bin/3to1go-edge-entrypoint
RUN sed -i 's/\r$//' /usr/local/bin/3to1go-edge-entrypoint \
    && chmod +x /usr/local/bin/3to1go-edge-entrypoint

EXPOSE 6556

ENTRYPOINT ["/usr/local/bin/3to1go-edge-entrypoint"]
CMD ["/usr/local/bin/3to1go-edge"]
