"""AgentK 官方 Client SDK 的同步封装。

## 这一层为什么存在

SDK 只提供 ``AsyncClient``，而质检链路是同步的，而且跑在 ThreadPoolExecutor
里（合并扫描按分片并发）。这里用**一条常驻事件循环线程**把 async 桥成 sync：
所有工作线程把协程投给同一个 loop，共用一份连接池，比"每次调用 asyncio.run"
少掉每次重建 TLS 连接的开销。

## 为什么用 InvokeCapability 而不是 Session + Execution

``InvokeCapability``（SDK 0.3.2 起）是同步阻塞式直调：给 capability id 和一个
JSON 输入，等它跑完直接拿输出。原先那套 CreateSession → StartExecution →
轮询 GetExecution 除了三倍 RPC 之外，还得为"一个会话同时只能有一个活跃执行"
做按线程隔离的会话池，全部可以不要了。

**代价是一条硬上限：网关对同步调用的容忍是 300 秒**（实测：给
understand_media 一个不可达 URL，恰好 300.0s 后回 UNAVAILABLE / Gateway
Timeout），而 understand_media 自己的 capability 超时是 360 秒。也就是说
300~360 秒之间存在一个区间，能力本身还在跑、同步调用却已经被网关切断。
观测到的真实耗时最长在 4b 主判（约 137s），有两倍余量；但更长的视频有可能
逼近上限，所以 ``AgentKCallError`` 会把这种情况单独措辞，别让它看起来像
一次普通的服务抖动。

## 鉴权

不做任何事。SDK 的 ``identity_headers()`` 直接读 ``KWS_SERVICE_NAME`` /
``KWS_SERVICE_CREDENTIAL`` 环境变量生成 ``kws-bin`` 头，与本仓库
``kws_identity.py`` 的实现逐字节一致（那份保留下来是给跨语言基准测试用的）。
本地只需要 export 服务名，不需要 API Key，也不需要 kess-agent——网关按
``KWS_SERVICE_STAGE`` 推导（STAGING/CANDIDATE 走 staging，其余走生产）。
"""

from __future__ import annotations

import asyncio
import threading
from concurrent.futures import TimeoutError as FutureTimeout
from typing import Any


# 网关对一次同步 InvokeCapability 的上限。不是我们设的、也改不了，
# 只用于把"被网关切断"和"能力真的失败"在错误信息里区分开。
GATEWAY_SYNC_LIMIT_SECONDS = 300.0

# Connect 错误码里值得重试的那些。故意不含 unknown：平台把 capability 内部的
# 业务错误（模型不可用、参数不合法）都塞进 unknown，重试只是白烧一次调用。
_RETRYABLE_CODES = frozenset(
    {"unavailable", "deadline_exceeded", "resource_exhausted", "aborted"}
)

_sdk_lock = threading.Lock()
_sdk: Any = None


def _load_sdk() -> Any:
    """惰性加载 ks-agentk-client。

    不在模块顶层 import 的原因：``judge/pipeline.py`` 无条件 import AgentKClient，
    顶层 import 会让**任何** import pipeline 的动作都要求装上 SDK，而 SDK 只在公司
    内部源上（还带一串 infra-* 传递依赖），受限环境里装不上。延迟到真正发起调用时
    才要求它，离线分析脚本（batch/eval_vs_human 等只读结果缓存的那些）才跑得起来。
    """
    global _sdk
    with _sdk_lock:
        if _sdk is None:
            try:
                from types import SimpleNamespace

                from agentk.api.v1.service_pb import InvokeCapabilityRequest
                from agentk.client import AsyncClient, Options
                from agentk.client.grpc import create_client as create_grpc_client
                from agentk.client.http import create_client as create_http_client
                from connectrpc.errors import ConnectError
                from protobuf.wkt import Struct
            except ImportError as error:
                raise RuntimeError(
                    "AgentK 通道需要官方 Client SDK：uv sync --extra agentk"
                    "（它只在公司内部 PyPI 上；只读结果缓存的离线脚本不需要它）"
                ) from error
            _sdk = SimpleNamespace(
                InvokeCapabilityRequest=InvokeCapabilityRequest,
                AsyncClient=AsyncClient,
                Options=Options,
                create_http_client=create_http_client,
                create_grpc_client=create_grpc_client,
                ConnectError=ConnectError,
                Struct=Struct,
            )
        return _sdk


class AgentKCallError(RuntimeError):
    """一次 capability 调用失败。

    带上 ``retryable``，让调用方不必再去 match 错误信息字符串——原先
    ``_is_retryable`` 靠 "HTTP 429" 之类的措辞做分流，换传输层就会静默失效。
    """

    def __init__(self, message: str, *, code: str = "", retryable: bool = False):
        super().__init__(message)
        self.code = code
        self.retryable = retryable


class _LoopThread:
    """常驻事件循环线程。

    daemon 线程 + 不主动 join：进程退出时循环里不应该还有在飞的调用，
    真有的话也不值得为它阻塞退出。
    """

    def __init__(self) -> None:
        self._loop = asyncio.new_event_loop()
        self._thread = threading.Thread(
            target=self._run, name="agentk-transport", daemon=True
        )
        self._thread.start()

    def _run(self) -> None:
        asyncio.set_event_loop(self._loop)
        self._loop.run_forever()

    def submit(self, coro: Any, timeout: float) -> Any:
        future = asyncio.run_coroutine_threadsafe(coro, self._loop)
        try:
            return future.result(timeout=timeout)
        except FutureTimeout:
            # 放弃等待不等于请求结束：不 cancel 的话这个协程会继续持有连接，
            # 批量模式下几次超时就能把连接池占满。
            future.cancel()
            raise

    def stop(self) -> None:
        self._loop.call_soon_threadsafe(self._loop.stop)


class AgentKTransport:
    """一份客户端与一条事件循环线程，对应一个 (通道, endpoint) 组合。"""

    def __init__(self, endpoint: str = "", *, transport: str = "http") -> None:
        if transport not in ("http", "grpc"):
            raise ValueError(f"未知的 AgentK 传输通道：{transport!r}（只支持 http / grpc）")
        self.endpoint = endpoint
        self.transport = transport
        self._sdk = _load_sdk()
        self._loop = _LoopThread()
        if transport == "grpc":
            # endpoint 必须留空才会触发 KESS 发现 klingai-agentk-api；给了地址
            # SDK 就当成直连覆盖，那就绕开了发现、也就回到了不通的固定网关。
            self._client = self._sdk.create_grpc_client(self._sdk.Options())
        else:
            self._client = self._sdk.create_http_client(
                self._sdk.Options(endpoint=endpoint)
            )

    def invoke(
        self, capability_id: str, payload: dict[str, Any], *, timeout: float
    ) -> Any:
        """直调一次 capability，返回它的输出（通常是 dict）。

        timeout 是我们这侧的等待上限；真正卡死同步调用的是网关的 300s，
        所以给得比它大也不会让调用活得更久。
        """
        request = self._sdk.InvokeCapabilityRequest(
            capability_id=capability_id, input=self._sdk.Struct.from_python(payload)
        )

        async def _call() -> Any:
            response = await self._client.invoke_capability(request)
            return response.output.to_python() if response.output is not None else None

        try:
            return self._loop.submit(_call(), timeout=timeout)
        except FutureTimeout as error:
            raise AgentKCallError(
                f"AgentK {capability_id} 等待超时（{timeout:.0f}s）",
                code="deadline_exceeded",
                retryable=True,
            ) from error
        except self._sdk.ConnectError as error:
            raise _translate(capability_id, error) from error

    def close(self) -> None:
        try:
            self._loop.submit(self._client.aclose(), timeout=10.0)
        except Exception:  # noqa: BLE001 - 关闭失败不该影响调用方的收尾
            pass
        self._loop.stop()


def _translate(capability_id: str, error: Exception) -> AgentKCallError:
    """把 ConnectError 翻成带重试语义的本地异常。"""
    code = getattr(error.code, "value", str(error.code))
    detail = str(error).strip()
    if code == "unavailable" and "Gateway Timeout" in detail:
        return AgentKCallError(
            f"AgentK {capability_id} 被网关切断：同步直调上限 "
            f"{GATEWAY_SYNC_LIMIT_SECONDS:.0f}s（能力本身超时 360s，"
            "素材过大或视频过长时会落进这个区间）",
            code=code,
            retryable=True,
        )
    return AgentKCallError(
        f"AgentK {capability_id} 失败（{code}）：{detail[:600]}",
        code=code,
        retryable=code in _RETRYABLE_CODES,
    )


_shared_lock = threading.Lock()
_shared: dict[tuple[str, str], AgentKTransport] = {}


def shared_transport(endpoint: str = "", *, transport: str = "http") -> AgentKTransport:
    """按 (通道, endpoint) 复用进程级传输。

    批量模式下每个 case 各建一个 AgentKClient，但它们打的是同一个网关；
    共用一份客户端才能让连接复用真的发生，也避免线程数随 case 数增长。

    键里必须带通道：同一个 endpoint 字符串在 http 与 grpc 下含义不同（grpc 忽略它
    走 KESS），只按 endpoint 缓存会让先建的那种通道被后来者复用。
    """
    key = (transport, endpoint)
    with _shared_lock:
        existing = _shared.get(key)
        if existing is None:
            existing = AgentKTransport(endpoint, transport=transport)
            _shared[key] = existing
        return existing


def transport_for(config: Any) -> AgentKTransport:
    """按配置取传输。调用方都该走这个，别自己拼通道与 endpoint。

    收口的意义在于：通道怎么选是环境相关的知识（生产必须 KESS gRPC，见
    ``config.default_agentk_transport``），散在各调用点就会有人漏掉，而漏掉的
    表现是 ``unauthorized``——和"没授权"分不开。
    """
    return shared_transport(config.base_url, transport=config.transport)
