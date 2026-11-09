# 容器云 kcsize supervisor 模式；agentk-api 监听 AUTO_PORT0。

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
# agentk-admin 仅供容器内带外管理，不由 supervisor 启动。
RUN go build -trimpath -ldflags "-s -w" -o output/bin/agentk-api ./cmd/agentk-api && \
    go build -trimpath -ldflags "-s -w" -o output/bin/agentk-admin ./cmd/agentk-admin && \
    cp deploy/container/start.sh output/bin/ && \
    chmod a+x output/bin/start.sh

# 静态 ffmpeg 提供视频合成所需的 libx264 与 subtitles filter。
FROM registry.corp.kuaishou.com/ksbase/build/golang/golang:1.26-alpine-20260601 AS ffmpeg
RUN wget -qO /tmp/ffmpeg.tar.xz \
      https://cdnfile.corp.kuaishou.com/kc/files/a/krow-internal/assets/ffmpeg/ffmpeg-release-7.0.2-amd64-static.tar.xz && \
    echo "abda8d77ce8309141f83ab8edf0596834087c52467f6badf376a6a2a4c87cf67  /tmp/ffmpeg.tar.xz" | sha256sum -c - && \
    tar -xf /tmp/ffmpeg.tar.xz -C /tmp && \
    mv /tmp/ffmpeg-7.0.2-amd64-static/ffmpeg /ffmpeg && \
    mv /tmp/ffmpeg-7.0.2-amd64-static/ffprobe /ffprobe && \
    /ffmpeg -hide_banner -version && \
    /ffprobe -hide_banner -version && \
    /ffmpeg -hide_banner -encoders 2>/dev/null | grep -q libx264 && \
    /ffmpeg -hide_banner -filters 2>/dev/null | grep -q ' subtitles ' && \
    rm -rf /tmp/ffmpeg*

FROM registry.corp.kuaishou.com/ksbase/runtime/golang/kcsize-slim:1.0-20260807

COPY --from=builder /build/output /home/web_server/agentk
COPY --from=ffmpeg /ffmpeg /usr/local/bin/ffmpeg
COPY --from=ffmpeg /ffprobe /usr/local/bin/ffprobe
WORKDIR /home/web_server/agentk

# kcsize 根据以下变量生成 supervisor 配置。
ENV SUPERVISOR_PROGRAM0=agentk-api \
    SUPERVISOR_COMMAND0=/home/web_server/agentk/bin/start.sh

# kcsize 使用 /healthz 检查并注册 AUTO_PORT0。
ENV SERVER_TYPE=http \
    PREHEAT_URL=/healthz \
    HEALTH_CHECK_URL=/healthz \
    HEALTH_CHECK_TIMEOUT=5
