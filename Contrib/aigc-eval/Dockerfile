# 容器云「自定义 / 用户自定义服务」部署镜像：AIGC 质检 worker + HTTP 双进程。
#
# 这里曾经还有一个纯 HTTP 的镜像，前提是 studio 在自己进程里跑判据、直接调
# understand_media。那条路已经下了：studio 现在只是 video_judge capability 的调用方，
# 判据在全公司只有 worker 这一份。没有 worker 的镜像因此起得来却判不了——而且是在
# 健康检查全绿的情况下失败，所以那个镜像不是「少一个形态」，是个陷阱，已删除。
#
# 只剩一个形态，所以文件名就是 Dockerfile，不带后缀：KDev 的构建插件默认找
# 项目根的 ``Dockerfile``、构建上下文默认 ``.``，兄弟项目（ffmpeg-worker、
# agentk-capability-demo、maas-worker）也都是这个约定。带后缀等于每条流水线都得
# 记着改一个字段，而改漏时的报错是「找不到 Dockerfile」，与代码问题无关却要人来查。
#
# 基础镜像用容器云的 Python + uv 镜像（与 agentk-capability-demo 同一个）：
# worker extra 依赖 infra-* 一族，只在公司内部 PyPI 上，公网源解析不到；这个镜像
# 已经配好内部源，也带得动 cchardet 的现场编译。
#
# 依赖安装必须用 uv 而不是 pip：infra-* 对 cityhash / python-snappy / lz4 的版本
# 上限已经过时，指向的老版本在现代 Python 上编不过；顶掉这些上限用的是
# pyproject.toml 的 tool.uv.override-dependencies，而它与 extra-build-dependencies
# （给 cchardet 补 cython、给 dsc-auth 补 setuptools_scm）都是 uv 独有的能力。
#
# 从本目录构建：
#   podman build --platform linux/amd64 -t aigc-eval-worker:dev .

FROM registry.corp.kuaishou.com/ksp/public/private_clouds/container-cloud/container-cloud-python:1.0.50-kwaios-python313-uv-c AS builder

ENV UV_PROJECT_ENVIRONMENT=/opt/venv \
    UV_LINK_MODE=copy \
    UV_COMPILE_BYTECODE=1 \
    UV_HTTP_TIMEOUT=600 \
    UV_CONCURRENT_DOWNLOADS=4

WORKDIR /build
# 带上 uv.lock 并用 --locked：worker 这条依赖链里有需要现场编译的 sdist，不锁版本
# 的话某个 infra-* 包发新版就可能让镜像构建在无关的时间点突然失败。
# --no-install-project：业务代码用 PYTHONPATH 挂载，改代码不必重装依赖层。
COPY pyproject.toml uv.lock README.md ./
RUN uv sync --extra worker --no-dev --locked --no-install-project

# 静态 ffmpeg：与 agentk/Dockerfile 同一份产物、同一个校验和。ffmpeg/ffprobe 是
# 质检的硬依赖——确定性前置检查（黑帧、卡顿、时长）、运动感知抽帧、证据帧标注、
# V2V 的 SSIM 对齐、以及音轨提取全靠它，基础镜像里没有。
FROM registry.corp.kuaishou.com/ksp/public/private_clouds/container-cloud/container-cloud-python:1.0.50-kwaios-python313-uv-c AS ffmpeg
RUN set -eux; \
    curl -fsSL -o /tmp/ffmpeg.tar.xz \
      https://cdnfile.corp.kuaishou.com/kc/files/a/krow-internal/assets/ffmpeg/ffmpeg-release-7.0.2-amd64-static.tar.xz; \
    echo "abda8d77ce8309141f83ab8edf0596834087c52467f6badf376a6a2a4c87cf67  /tmp/ffmpeg.tar.xz" | sha256sum -c -; \
    tar -xf /tmp/ffmpeg.tar.xz -C /tmp; \
    mv /tmp/ffmpeg-7.0.2-amd64-static/ffmpeg /ffmpeg; \
    mv /tmp/ffmpeg-7.0.2-amd64-static/ffprobe /ffprobe; \
    /ffmpeg -hide_banner -version; \
    /ffprobe -hide_banner -version; \
    rm -rf /tmp/ffmpeg*

FROM registry.corp.kuaishou.com/ksp/public/private_clouds/container-cloud/container-cloud-python:1.0.50-kwaios-python313-uv-c

ENV LANG=C.UTF-8 \
    LC_ALL=C.UTF-8 \
    TZ=Asia/Shanghai \
    PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    LOG_PATH=/home/web_server/kuaishou-runner/logs

# runtime/ 必须可写且世界可写：报告、抽帧、上传件都落在这里，而容器云跑哪个 uid
# 由平台决定。路径由 src/aigc_eval/paths.py 按源码位置推出（src 的上一级），
# 所以它跟着 APP_HOME 走，不能随便挪源码层级。
RUN set -eux; \
    ln -snf /usr/share/zoneinfo/$TZ /etc/localtime; \
    echo "$TZ" > /etc/timezone; \
    mkdir -p /home/web_server/aigc-eval/runtime "$LOG_PATH"; \
    chmod 0777 /home/web_server/aigc-eval/runtime "$LOG_PATH"; \
    curl -fsSL http://bs3-hb1.internal/image-kcsize/kcsize -o /usr/local/bin/kcsize; \
    chmod a+x /usr/local/bin/kcsize; \
    ln -sf /usr/local/bin/kcsize /usr/local/sbin/kcsize; \
    rm -rf /tmp/* /var/tmp/*

COPY --from=builder /opt/venv /opt/venv
COPY --from=ffmpeg /ffmpeg /usr/local/bin/ffmpeg
COPY --from=ffmpeg /ffprobe /usr/local/bin/ffprobe
ENV VIRTUAL_ENV=/opt/venv \
    PATH=/opt/venv/bin:${PATH}

WORKDIR /home/web_server/aigc-eval
COPY pyproject.toml README.md ./
COPY src ./src
COPY worker ./worker
COPY deploy/container/start.sh /start.sh
COPY deploy/container/stop.sh ./bin/stop.sh
# 诊断脚本要在目标环境里手工跑（见各自文件头），所以必须随镜像走。
# container_env.py 是它们共用的前置（借服务进程的身份），三个文件同目录才能 import。
COPY deploy/container/verify_deployment.py ./bin/verify_deployment.py
COPY deploy/container/smoke_check.py ./bin/smoke_check.py
COPY deploy/container/whoami_probe.py ./bin/whoami_probe.py
COPY deploy/container/container_env.py ./bin/container_env.py
RUN chmod a+x /start.sh ./bin/stop.sh

ENV PYTHONPATH=/home/web_server/aigc-eval/src \
    WORKER_DIR=/home/web_server/aigc-eval/worker \
    VIDEO_JUDGE_PROVIDER=agentk

# 贴合 understand_media 的 mediaUrls 上限，避免素材被降载丢弃。
ENV JUDGE_UNIFIED_SCAN_FRAMES=9

# 构建期冒烟：把 worker 与 HTTP 两侧的顶层 import 链都走一遍，并确认 ffmpeg 在位。
# 缺依赖、Python 版本不兼容在 build 时就暴雷，不拖到上线才发现。
#
# 末尾要重新放开 runtime/ 权限：import 链在构建期就会按 paths.py 建出 reports/
# work/ 等子目录，它们归 root 且 0755，而容器云跑哪个 uid 由平台定——不修这一下，
# 表现是服务起得来、质检跑到写报告那步才 Permission denied。
RUN set -eux; \
    ffmpeg -hide_banner -version > /dev/null; \
    ffprobe -hide_banner -version > /dev/null; \
    python3 -c 'import agentk, agentk.worker, pydantic; import aigc_eval.server.app'; \
    (cd worker && python3 -c 'import worker; print("aigc-eval worker import smoke test OK")'); \
    chmod -R a+rwX runtime

# 接入信息不在镜像里写死，分两条不同的通道，别混：
#
# 1. worker 连 Temporal：走 SDK 的 connect_temporal()，由 KESS 发现服务，依赖容器云
#    内置的 kess-agent。别在部署配置里设 TEMPORAL_ADDRESS——基线实例会忽略它，
#    但残留这个变量在联调实例上会让 worker 静默连到空 namespace。
# 2. 质检调 understand_media：走官方 Client SDK 的 HTTP transport，身份是天琴服务名
#    （本服务为 klingai-agentk-aigc-eval-api）。KWS_SERVICE_NAME / KWS_SERVICE_STAGE /
#    KWS_SERVICE_CREDENTIAL 由平台注入，网关按 stage 推导；不要手工设 AGENTK_BASE_URL。
#
# 另外 AIGC_EVAL_BLOB_ENDPOINT / AIGC_EVAL_BLOB_BUCKET 必须在部署单元里显式配置，
# 它们不由平台注入。缺了的话基线实例拒绝启动（退出码 2），见 worker/worker.py。

# kcsize 启动后执行 START_COMMAND，停止时执行 STOP_COMMAND。
# 不用 SUPERVISOR_PROGRAMx 分别托管两个进程：worker 与 HTTP 必须同生共死，
# 让 supervisor 单独重拉其中一个，会掩盖「能力已从目录消失但健康检查仍绿」的状态。
ENV START_COMMAND=/start.sh \
    STOP_COMMAND=/home/web_server/aigc-eval/bin/stop.sh

CMD ["/usr/local/bin/kcsize", "api"]
