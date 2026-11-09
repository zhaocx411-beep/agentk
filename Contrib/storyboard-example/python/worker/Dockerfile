# Build context: storyboard-example repository root.
FROM registry.corp.kuaishou.com/ksbase/build/python/python:3.13.15 AS build

RUN apt-get update \
    && apt-get install -y --no-install-recommends libsnappy-dev \
    && rm -rf /var/lib/apt/lists/* \
    && python -m pip install --no-cache-dir uv==0.11.8

ENV UV_COMPILE_BYTECODE=1 \
    UV_LINK_MODE=copy \
    UV_PYTHON_DOWNLOADS=never \
    UV_PROJECT_ENVIRONMENT=/opt/venv

WORKDIR /build/python
COPY python/pyproject.toml python/uv.lock ./
COPY python/worker/pyproject.toml worker/pyproject.toml
COPY python/mcp-server/pyproject.toml mcp-server/pyproject.toml
RUN uv sync --locked --no-dev --python /usr/local/bin/python3.13 \
    --package storyboard-example-worker

FROM registry.corp.kuaishou.com/ksbase/runtime/python/kcsize:3.13.15-kcsize1.0.0-trixie-slim-r1

ENV PATH="/opt/venv/bin:$PATH"

WORKDIR /home/web_server/storyboard-example/python/worker
COPY --from=build /opt/venv /opt/venv
COPY python/worker/abi.py python/worker/workflow.py python/worker/worker.py ./

ENV SUPERVISOR_PROGRAM0=storyboard-example-python-worker \
    SUPERVISOR_COMMAND0="/opt/venv/bin/python /home/web_server/storyboard-example/python/worker/worker.py"
