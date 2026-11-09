# 容器云「自定义 / 用户自定义服务」部署镜像：Agent Studio API Server
# （含漫剧工坊与全部 chat Agent；漫剧默认 DRAMA_MODE=agent，只依赖 AgentK Connect API）。
# 接入文档：TechLink《自定义服务-Go语言服务接入》
#
# 多阶段构建：公司 Go 构建镜像静态编译；运行阶段用 kcsize-slim 运行时镜像
# （已内置 kcsize + supervisord 及 /etc/supervisor 骨架，不必像 agent-k 的
# Dockerfile 那样在 ubuntu 上手工内化这套约定）。
#
# 从仓库根目录构建：
#   podman build --platform linux/amd64 \
#     -f deploy/container/Dockerfile -t comic-drama-agent:dev .

FROM registry.corp.kuaishou.com/ksbase/build/golang/golang:1.26-alpine-20260601 AS builder

# 私有 SDK（agentk-sdk、infra-framework-go）必须经公司 GOPROXY 拉取；
# GONOPROXY=none 防止基础镜像预置的 GOPRIVATE 让 git.corp 模块绕过代理
# 直连 git（构建镜像里没有 git 也没有凭证，直连必失败，表现为
# "missing Git command" / "no secure protocol found"）。
ARG GOPROXY=https://goproxy.corp.kuaishou.com
ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64 \
    GOPROXY=${GOPROXY} \
    GONOPROXY=none \
    GOSUMDB=off

WORKDIR /build
# 注意：catalog.go 的 agentOrder 里登记的每个 Agent 目录都在 agents/ 下。
# go:embed 用通配符，缺目录编译不会报错，但进程启动时 MustAgents() 会
# panic（读不到 meta.json），表现为容器反复崩溃、健康检查 connection refused。
COPY go.mod go.sum catalog.go ./
COPY cmd/ ./cmd/
COPY platform/ ./platform/
COPY agents/ ./agents/
COPY deploy/container/start.sh output/bin/start.sh
COPY deploy/container/worker-start.sh output/bin/worker-start.sh
RUN go build -trimpath -ldflags "-s -w" \
        -o output/bin/comic-drama-apiserver ./cmd/apiserver && \
    go build -trimpath -ldflags "-s -w" \
        -o output/bin/comic-drama-worker ./cmd/drama-worker && \
    chmod a+x output/bin/start.sh output/bin/worker-start.sh

FROM registry.corp.kuaishou.com/ksbase/runtime/golang/kcsize-slim:1.0-20260807

ENV LANG=C.UTF-8 \
    LC_ALL=C.UTF-8 \
    TZ=Asia/Shanghai \
    LOG_PATH=/home/web_server/kuaishou-runner/logs

# 前端与漫剧 profile 都在编译期内嵌进二进制：镜像里只有 bin，
# 无需再分发 profile / web 静态目录。热修 prompt 时用 PROFILE_FILE 指盘上文件。
COPY --from=builder /build/output/bin /home/web_server/comic-drama/bin
WORKDIR /home/web_server/comic-drama

# 漫剧缺省即 agent 模式（不需要部署 Temporal Worker）。
# AGENTK_BASE_URL / AGENTK_API_KEY（Service Principal Key）等由容器云
# 环境变量下发，缺失时对 agent-k 的调用会失败，进程自己报错，这里不重复校验。
ENV DRAMA_MODE=agent

# kcsize supervisor 模式：1 号进程按 SUPERVISOR_PROGRAMx/COMMANDx 生成
# supervisord program 配置并拉起；优雅停止走 stopsignal=TERM
# （apiserver 自身处理 SIGTERM），进程挂掉由 autorestart 自动重拉。
#
# PROGRAM1 是 drama-worker：注册 call_agent 路由能力与漫剧 pipeline，
# 统一对话入口（assistant）依赖它。与 apiserver 同容器部署省去第二个
# 服务；需要独立伸缩时改用 worker.Dockerfile 单独部署并删掉 PROGRAM1。
ENV SUPERVISOR_PROGRAM0=comic-drama-apiserver \
    SUPERVISOR_COMMAND0=/home/web_server/comic-drama/bin/start.sh \
    SUPERVISOR_PROGRAM1=comic-drama-worker \
    SUPERVISOR_COMMAND1=/home/web_server/comic-drama/bin/worker-start.sh

# SERVER_TYPE=http：kcsize 对 AUTO_PORT0 做 HTTP 检查并注册 KNS。
# /healthz 返回体恰为 kcsize 默认匹配值 "ok"（见 platform/app/app.go）。
ENV SERVER_TYPE=http \
    PREHEAT_URL=/healthz \
    HEALTH_CHECK_URL=/healthz \
    HEALTH_CHECK_TIMEOUT=5

CMD ["/usr/local/bin/kcsize"]
