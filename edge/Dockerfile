FROM python:3.12-slim AS builder

WORKDIR /srv/edge

RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential libffi-dev \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .
RUN pip install --no-cache-dir --prefix=/install -r requirements.txt

FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1
ENV XDG_CONFIG_HOME=/config
ENV XDG_STATE_HOME=/data/state
ENV XDG_CACHE_HOME=/data/cache

WORKDIR /srv/edge

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl libffi8 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /install /usr/local
COPY app ./app
COPY docker-entrypoint.sh /usr/local/bin/relay-edge-entrypoint
RUN sed -i 's/\r$//' /usr/local/bin/relay-edge-entrypoint \
    && chmod +x /usr/local/bin/relay-edge-entrypoint

EXPOSE 6556

ENTRYPOINT ["relay-edge-entrypoint"]
CMD ["python", "-m", "app.main"]
