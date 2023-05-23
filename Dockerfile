FROM python:3.9-alpine3.15 AS build-env

ENV PYTHONFAULTHANDLER=1 \
    PYTHONUNBUFFERED=1 \
    PYTHONHASHSEED=random \
    PIP_NO_CACHE_DIR=off \
    PIP_DISABLE_PIP_VERSION_CHECK=on \
    PIP_DEFAULT_TIMEOUT=100 \
    POETRY_NO_INTERACTION=1

RUN apk add --no-cache --virtual .python_deps build-base python3-dev libffi-dev gcc bash && \
    pip3 install poetry && \
    apk add --no-cache git && \
    mkdir -p /app/src /app /shared && \
    poetry config virtualenvs.create false

ADD pyproject.toml /app/pyproject.toml

WORKDIR /app

RUN poetry export --dev --without-hashes --no-interaction --no-ansi -f requirements.txt -o requirements.txt && \
    pip install --force-reinstall -r requirements.txt && \
    apk del .python_deps

ADD src /app/src

WORKDIR /app

ENTRYPOINT python3 /app/src/blockgametracker/main.py
