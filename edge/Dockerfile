FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1

WORKDIR /srv/edge

COPY edge/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY edge/app ./app

CMD ["python", "-m", "app.main"]

