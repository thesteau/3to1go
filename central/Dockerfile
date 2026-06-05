FROM golang:1.25-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /central ./cmd/server

FROM alpine:3.21

RUN apk add --no-cache ca-certificates curl

ENV XDG_CONFIG_HOME=/config

WORKDIR /srv/central

COPY --from=builder /central /usr/local/bin/central
COPY docker-entrypoint.sh /usr/local/bin/relay-central-entrypoint
RUN chmod +x /usr/local/bin/relay-central-entrypoint

EXPOSE 6555

ENTRYPOINT ["relay-central-entrypoint"]
CMD ["/usr/local/bin/central"]
