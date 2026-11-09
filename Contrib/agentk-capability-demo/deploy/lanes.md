# AgentK API 泳道接入

demo 已有 ASGI 入口适配器，继续读取 KS `trace-context` 中的 `laneId`。
PRT 部署设置 `KWS_SERVICE_STAGE=PROD_REGRESSION_TEST`；Worker 同时设置 `KWS_LANE_ID=prt`。
自定义 lane 优先从入口 carrier 获取。本地缺少 carrier 时，可使用现有的
`FALLBACK_TO_ENV_LANE_ID=true` 与 `KWS_LANE_ID=<开发泳道>`。

本次 Python 流水线按 workspace 发布全部四个包，因此 client、protocol、request-lane、
worker 必须统一使用 0.3.7.dev3，避免显式依赖约束冲突。
自定义 `FixedAddressProvider.acquire` 已接受 lane 参数；显式地址继续直连，SDK 负责染色。

## 发布后切换依赖

四个 Python SDK 包发布到 prod 后，统一更新依赖并重新生成 lockfile：

```bash
# VERSION 使用已发布的 Python 预发布版本，例如 0.3.7.dev3。
uv add "ks-agentk-client[grpc]==$VERSION" \
  "ks-agentk-protocol==$VERSION" \
  "ks-agentk-request-lane==$VERSION" \
  "ks-agentk-worker==$VERSION" \
  --index kuaishou=https://pypi.corp.kuaishou.com/kuaishou/prod/+simple/
uv sync --locked
uv run pytest -q
```

提交更新后的 pyproject.toml 与 uv.lock 后再构建镜像。不将本地源码路径写入部署依赖。
本次源码联调已经覆盖实际 Connect 传输的 baseline、PRT 默认值及显式开发 lane。

## 平台验收

先部署 PRT AgentK API，再升级 demo。KESS 模式不传 `--agentk-address`：baseline 请求
选 baseline API，PRT 请求选 PRT API，开发 lane 优先同 lane API、缺少时选 PRT API。
均无可用实例时应报错。HTTP 直连验证使用 `--agentk-address <PRT API 地址>`；本期不改网关。

PROD Worker A 提供完整能力，PRT Worker B 只发布两个修改能力：PRT 列表和调用均应优先
使用 B 的两个实现，其余来自 A。B 的执行失败不能重试 A。保留 baseline Core Worker，
继续使用现有注册、解析和嵌套调用链路。Narrato 全局 ASR limiter 的 baseline Worker
依赖也保持现状。
