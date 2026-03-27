FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1

WORKDIR /srv/central

COPY central/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY central/app ./app

EXPOSE 8000

CMD ["python", "-m", "app.main"]

