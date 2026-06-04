FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1
ENV XDG_CONFIG_HOME=/config

WORKDIR /srv/central

COPY requirements.txt .
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
RUN pip install --no-cache-dir -r requirements.txt

COPY app ./app
COPY docker-entrypoint.sh /usr/local/bin/relay-central-entrypoint
RUN chmod +x /usr/local/bin/relay-central-entrypoint

EXPOSE 6555

ENTRYPOINT ["relay-central-entrypoint"]
CMD ["python", "-m", "app.main"]
