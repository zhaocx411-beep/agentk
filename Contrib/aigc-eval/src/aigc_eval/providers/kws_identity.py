"""KWS 服务身份请求头（AgentK 新鉴权机制）。

AgentK 从 SDK 0.3.0（提交 4827290 "propagate KESS identity from SDK clients"）
起不再用 API Key，改成由调用方带上自己的 KWS 服务身份，服务端凭服务名授权。

这里刻意不引入 ks-agentk-client：在 0.3.0 里鉴权和传输是解耦的，无论走 gRPC+KESS
还是走 HTTP 网关，客户端发出的都只是下面这两个头（见 SDK 的
python/client/src/agentk/client/_core.py 里 ``AsyncClient.__init__``）。gRPC 那条路
额外解决的是「实例发现」，而本项目直连网关 URL，用不到，所以没必要为此背上
pyqwest / connectrpc / infra-kess 一串依赖，可以继续保持零三方依赖。

编码格式与 SDK 的 ``_identity.py`` 逐字节一致，测试用的就是 SDK 自带的
跨语言基准向量（见 tests/test_kws_identity.py）。
"""

from __future__ import annotations

import base64
import os
from collections.abc import Callable, Mapping

# 平台注入的环境变量名。线上由部署平台自动注入，本地自己 export。
KWS_SERVICE_NAME_ENV = "KWS_SERVICE_NAME"
KWS_SERVICE_CREDENTIAL_ENV = "KWS_SERVICE_CREDENTIAL"


def _encode_varint(value: int) -> bytes:
    """protobuf 的 base-128 varint。服务名超过 127 字节时长度就不止一个字节了。"""
    encoded = bytearray()
    while value >= 0x80:
        encoded.append((value & 0x7F) | 0x80)
        value >>= 7
    encoded.append(value)
    return bytes(encoded)


def _kws_model(service_name: str) -> bytes:
    """把服务名编成 KWS 的身份 message：字段 1（tag 0x0a）+ 长度 + UTF-8 值。

    只有一个字段，手写比拉 protobuf 依赖划算。
    """
    value = service_name.encode("utf-8")
    return b"\x0a" + _encode_varint(len(value)) + value


def identity_headers(
    getenv: Callable[[str], str | None] = os.getenv,
) -> dict[str, str]:
    """生成身份头；环境变量没配就返回空 dict（由调用方决定要不要回退到 API Key）。

    getenv 可注入，方便测试不污染进程环境。
    """
    headers: dict[str, str] = {}
    service_name = getenv(KWS_SERVICE_NAME_ENV) or ""
    credential = getenv(KWS_SERVICE_CREDENTIAL_ENV) or ""
    if service_name:
        # base64 去掉尾部 padding，与 gRPC 的 "-bin" 头约定一致。
        headers["kws-bin"] = (
            base64.b64encode(_kws_model(service_name)).decode("ascii").rstrip("=")
        )
    if credential:
        # 凭据原样透传，不做 strip：SDK 的基准向量保留了首尾空格。
        headers["kwsCredential"] = credential
    return headers


def describe_identity(headers: Mapping[str, str]) -> str:
    """给日志/健康检查用的一句话描述，不泄露凭据内容。"""
    if not headers:
        return "无"
    parts = ["kws-bin"]
    if "kwsCredential" in headers:
        parts.append("kwsCredential")
    return "+".join(parts)
