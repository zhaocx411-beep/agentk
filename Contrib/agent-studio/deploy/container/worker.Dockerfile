# 容器云「自定义 / 用户自定义服务」部署镜像：drama-worker
# （ComicDramaFilm pipeline + call_agent 路由能力）。
# 接入文档：TechLink《自定义服务-Go语言服务接入》
#
# 与 apiserver（同目录 Dockerfile）的差异：纯 Temporal Worker，无 HTTP
# 服务，因此不设 SERVER_TYPE / HEALTH_CHECK_URL，kcsize 不做端口注册；
# Temporal 经 KESS 自动发现（依赖容器云内置 kess-agent），无需配地址。
#
# 从仓库根目录构建：
#   podman build --platform linux/amd64 \
#     -f deploy/container/worker.Dockerfile -t comic-drama-worker:dev .

FROM registry.corp.kuaishou.com/ksbase/build/golang/golang:1.26-alpine-20260601 AS builder

# 私有 SDK（agentk-sdk、infra-framework-go）必须经公司 GOPROXY 拉取；
# GONOPROXY=none 防止基础镜像预置的 GOPRIVATE 让 git.corp 模块绕过代理
# 直连 git（构建镜像里没有 git 也没有凭证，直连必失败）。
ARG GOPROXY=https://goproxy.corp.kuaishou.com
ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64 \
    GOPROXY=${GOPROXY} \
    GONOPROXY=none \
    GOSUMDB=off

WORKDIR /build
COPY go.mod go.sum catalog.go ./
COPY cmd/ ./cmd/
COPY platform/ ./platform/
COPY agents/ ./agents/
COPY deploy/container/worker-start.sh output/bin/start.sh
RUN go build -trimpath -ldflags "-s -w" \
        -o output/bin/comic-drama-worker ./cmd/drama-worker && \
    chmod a+x output/bin/start.sh

FROM registry.corp.kuaishou.com/ksbase/runtime/golang/kcsize-slim:1.0-20260807

ENV LANG=C.UTF-8 \
    LC_ALL=C.UTF-8 \
    TZ=Asia/Shanghai \
    LOG_PATH=/home/web_server/kuaishou-runner/logs

COPY --from=builder /build/output/bin /home/web_server/comic-drama-worker/bin
WORKDIR /home/web_server/comic-drama-worker

# Temporal 与 AgentK 都由 SDK 经 KESS 自动发现；不要手工设置 AGENTK_BASE_URL、
# KWS_SERVICE_CREDENTIAL 或 KWS_SERVICE_STAGE。平台身份未绑定时 RPC 会失败。

# kcsize supervisor 模式：进程挂掉由 autorestart 自动重拉。
ENV SUPERVISOR_PROGRAM0=comic-drama-worker \
    SUPERVISOR_COMMAND0=/home/web_server/comic-drama-worker/bin/start.sh

CMD ["/usr/local/bin/kcsize"]
