# KCI / 容器云部署镜像，全部依赖来自公司内部（参考 agent-k-effects）：
# - 基础镜像：容器云 Python 3.13 + uv（registry.corp）
# - Python 包：公司内部 PyPI
# - agentk-worker：vendor/ 里的 wheel（不访问 Git，无需 BuildKit / --ssh）
#
#   docker build -t ffmpeg-worker:dev .
FROM registry.corp.kuaishou.com/ksp/public/private_clouds/container-cloud/container-cloud-python:1.0.50-kwaios-python313-uv-c AS builder

ENV UV_PROJECT_ENVIRONMENT=/opt/venv \
    UV_LINK_MODE=copy \
    UV_COMPILE_BYTECODE=1 \
    UV_HTTP_TIMEOUT=600 \
    UV_CONCURRENT_DOWNLOADS=4

WORKDIR /build
COPY pyproject.toml uv.lock ./
COPY vendor ./vendor
RUN set -eux; \
    python3 -c 'import sys; assert sys.version_info[:2] == (3, 13), sys.version'; \
    uv sync --locked --no-dev

FROM registry.corp.kuaishou.com/ksp/public/private_clouds/container-cloud/container-cloud-python:1.0.50-kwaios-python313-uv-c

ENV LANG=C.UTF-8 \
    LC_ALL=C.UTF-8 \
    TZ=Asia/Shanghai \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

COPY --from=builder /opt/venv /opt/venv
ENV VIRTUAL_ENV=/opt/venv \
    PATH=/opt/venv/bin:${PATH}

WORKDIR /app
COPY protos ./protos
COPY abi.py kess_ffmpeg.py activities.py workflow.py worker.py ./

# 构建期冒烟：把 worker 的顶层 import 链走一遍（含 agentk.worker / temporalio /
# kess 桥接 / pb2 桩），缺依赖在 build 时暴雷，不拖到上线。
RUN python3 -c 'import worker; print("ffmpeg-worker import smoke test OK")'

ENTRYPOINT ["python3", "worker.py"]
