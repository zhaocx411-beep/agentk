# 容器云「自定义 / 用户自定义服务」部署镜像。
# 使用容器云 Python 3.13 + uv 构建依赖环境。
# 依赖（含 AgentK SDK）统一走快手内部 PyPI，构建期不需要 git。
FROM registry.corp.kuaishou.com/ksp/public/private_clouds/container-cloud/container-cloud-python:1.0.50-kwaios-python313-uv-c AS builder

ENV UV_PROJECT_ENVIRONMENT=/opt/venv \
    UV_LINK_MODE=copy \
    UV_HTTP_TIMEOUT=600 \
    UV_CONCURRENT_DOWNLOADS=4

WORKDIR /build
COPY pyproject.toml uv.lock README.md ./
RUN set -eux; \
    python3 -c 'import sys; assert sys.version_info[:2] == (3, 13), sys.version'; \
    uv sync --locked --no-dev --no-install-project

FROM registry.corp.kuaishou.com/ksp/public/private_clouds/container-cloud/container-cloud-python:1.0.50-kwaios-python313-uv-c

ENV LANG=C.UTF-8 \
    LC_ALL=C.UTF-8 \
    TZ=Asia/Shanghai \
    PYTHONUNBUFFERED=1 \
    LOG_PATH=/home/web_server/kuaishou-runner/logs

RUN set -eux; \
    python3 -c 'import sys; assert sys.version_info[:2] == (3, 13), sys.version'; \
    command -v curl; \
    ln -snf /usr/share/zoneinfo/$TZ /etc/localtime; \
    echo "$TZ" > /etc/timezone; \
    mkdir -p \
        /home/web_server/agentk-capability-demo/bin \
        /home/web_server/agentk-capability-demo/uploads \
        "$LOG_PATH"; \
    curl -fsSL http://bs3-hb1.internal/image-kcsize/kcsize -o /usr/local/bin/kcsize; \
    chmod a+x /usr/local/bin/kcsize; \
    ln -sf /usr/local/bin/kcsize /usr/local/sbin/kcsize; \
    rm -rf /tmp/* /var/tmp/*

COPY --from=builder /opt/venv /opt/venv
ENV VIRTUAL_ENV=/opt/venv \
    PATH=/opt/venv/bin:${PATH}

COPY src/agentk_capability_demo /home/web_server/agentk-capability-demo/agentk_capability_demo
COPY config.narrato.example.yaml /home/web_server/agentk-capability-demo/config.narrato.example.yaml
WORKDIR /home/web_server/agentk-capability-demo
RUN python3 -c 'import fastapi, httpx, uvicorn; import agentk; import agentk.worker; import agentk_capability_demo.app; import agentk_capability_demo.agent100.narrato.worker'

COPY deploy/container/start.sh /start.sh
COPY deploy/container/stop.sh /home/web_server/agentk-capability-demo/bin/stop.sh
RUN chmod a+x /start.sh /home/web_server/agentk-capability-demo/bin/stop.sh

# kcsize 启动后执行 START_COMMAND，停止时执行 STOP_COMMAND。
ENV START_COMMAND=/start.sh \
    STOP_COMMAND=/home/web_server/agentk-capability-demo/bin/stop.sh

CMD ["/usr/local/bin/kcsize", "api"]
