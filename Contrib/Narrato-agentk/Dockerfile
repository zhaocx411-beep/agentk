# syntax=docker/dockerfile:1.7
# docker build --build-context agentk-sdk=../agentk-sdk -t narrato-agentk:latest .
FROM python:3.13-slim AS builder

ENV UV_LINK_MODE=copy

RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential \
    && rm -rf /var/lib/apt/lists/* \
    && pip install --no-cache-dir uv==0.8.13

WORKDIR /opt
COPY --from=agentk-sdk python/client /opt/agentk-sdk/python/client
COPY --from=agentk-sdk python/protocol /opt/agentk-sdk/python/protocol
COPY --from=agentk-sdk python/worker /opt/agentk-sdk/python/worker

WORKDIR /opt/narrato-agentk
COPY pyproject.toml uv.lock README.md LICENSE NOTICE ./
COPY src ./src
RUN uv sync --frozen --no-dev

FROM python:3.13-slim AS runtime

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PATH="/opt/narrato-agentk/.venv/bin:${PATH}"

WORKDIR /opt/narrato-agentk
COPY --from=builder /opt/agentk-sdk /opt/agentk-sdk
COPY --from=builder /opt/narrato-agentk /opt/narrato-agentk

EXPOSE 8000
CMD ["narrato-agentk-worker"]
