# 容器云 kcsize supervisor 模式；maas-worker 监听 AUTO_PORT0。

FROM registry.corp.kuaishou.com/ksbase/build/golang/golang:1.26-alpine-20260601 AS builder

# 私有模块必须经公司 GOPROXY；GONOPROXY=none 防止基础镜像配置绕过代理。
ENV GOPROXY=https://goproxy.corp.kuaishou.com \
    GONOPROXY=none \
    GOSUMDB=off \
    CGO_ENABLED=0 \
    GOOS=linux

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN mkdir -p output/bin && \
    go build -trimpath -ldflags "-s -w" -o output/bin/maas-gateway ./cmd/maas-gateway && \
    go build -trimpath -ldflags "-s -w" -o output/bin/maas-worker ./cmd/maas-worker && \
    cp deploy/container/start.sh output/bin/ && \
    chmod a+x output/bin/start.sh

FROM registry.corp.kuaishou.com/ksbase/runtime/golang/kcsize-slim:1.0-20260807

COPY --from=builder /build/output /home/web_server/maas-worker
WORKDIR /home/web_server/maas-worker

# kcsize 根据以下变量生成 supervisor 配置。
ENV SUPERVISOR_PROGRAM0=maas-worker \
    SUPERVISOR_COMMAND0=/home/web_server/maas-worker/bin/start.sh

# 两个部署单元复用镜像，但必须分别设置 MAAS_ROLE=gateway|worker。
# 两个角色都只在 AUTO_PORT0 暴露 /healthz；Worker 不暴露推理路由。
ENV SERVER_TYPE=http \
    PREHEAT_URL=/healthz \
    HEALTH_CHECK_URL=/healthz \
    HEALTH_CHECK_TIMEOUT=5
