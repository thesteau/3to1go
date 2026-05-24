FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1
ENV XDG_CONFIG_HOME=/config
ENV XDG_STATE_HOME=/data/state
ENV XDG_CACHE_HOME=/data/cache

WORKDIR /srv/edge

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY app ./app

EXPOSE 8080

CMD ["python", "-m", "app.main"]
