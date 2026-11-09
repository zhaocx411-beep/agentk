"""KWS 身份头编码测试。

用的全部是 AgentK SDK 0.3.0 自带的跨语言基准向量
（agentk-sdk/python/client/tests/test_identity.py），因为这里是手写复刻，
一旦编码差一个字节，服务端就会把请求判成无身份，而且只有连上 staging 才会
暴露出来。用同一组向量对齐，可以在离线状态下就保证与 SDK 兼容。
"""

from __future__ import annotations

import base64
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

from aigc_eval.providers.kws_identity import (  # noqa: E402
    _kws_model,
    describe_identity,
    identity_headers,
)

FAILURES: list[str] = []


def check(label: str, ok: bool, detail: object = "") -> None:
    print(f"{'PASS' if ok else 'FAIL'}  {label}" + (f" — {detail}" if not ok else ""))
    if not ok:
        FAILURES.append(label)


def main() -> int:
    # SDK 的跨语言基准：字段 1 + 长度 0x19(25) + UTF-8 服务名。
    fixture = "agentk.sdk.fixture/原样"
    raw = _kws_model(fixture)
    check(
        "字节编码与 SDK 基准一致",
        raw.hex() == "0a196167656e746b2e73646b2e666978747572652fe58e9fe6a0b7",
        raw.hex(),
    )
    check(
        "base64 去 padding 与 SDK 基准一致",
        base64.b64encode(raw).decode("ascii").rstrip("=")
        == "ChlhZ2VudGsuc2RrLmZpeHR1cmUv5Y6f5qC3",
        base64.b64encode(raw).decode("ascii"),
    )

    # 服务名超 127 字节要用多字节 varint，klingai-agentk-aigc-eval-api 远没到，
    # 但编码实现必须是对的，否则将来换长名字会静默失效。
    long_raw = _kws_model("x" * 128)
    check(
        "超长服务名使用多字节 varint",
        long_raw[:3] == b"\x0a\x80\x01" and long_raw[3:] == b"x" * 128,
        long_raw[:4],
    )

    # 环境变量组合。
    check("两个变量都没配时不产生任何头", identity_headers({}.get) == {})
    check(
        "只配服务名时只有 kws-bin",
        identity_headers({"KWS_SERVICE_NAME": fixture}.get)
        == {"kws-bin": "ChlhZ2VudGsuc2RrLmZpeHR1cmUv5Y6f5qC3"},
        identity_headers({"KWS_SERVICE_NAME": fixture}.get),
    )
    check(
        "凭据原样透传（保留首尾空格）",
        identity_headers({"KWS_SERVICE_CREDENTIAL": " credential 原样 "}.get)
        == {"kwsCredential": " credential 原样 "},
    )
    check(
        "服务名 + 凭据同时存在",
        identity_headers(
            {
                "KWS_SERVICE_NAME": "Service.Name/原样",
                "KWS_SERVICE_CREDENTIAL": "credential",
            }.get
        )
        == {
            "kws-bin": "ChNTZXJ2aWNlLk5hbWUv5Y6f5qC3",
            "kwsCredential": "credential",
        },
    )

    # 真实要用的服务名能编出来，且描述不泄露凭据。
    real = identity_headers(
        {
            "KWS_SERVICE_NAME": "klingai-agentk-aigc-eval-api",
            "KWS_SERVICE_CREDENTIAL": "s3cret",
        }.get
    )
    check("真实服务名可编码", bool(real.get("kws-bin")), real.get("kws-bin"))
    check(
        "describe_identity 不泄露凭据",
        describe_identity(real) == "kws-bin+kwsCredential"
        and "s3cret" not in describe_identity(real),
        describe_identity(real),
    )
    check("describe_identity 空身份", describe_identity({}) == "无")

    print()
    if FAILURES:
        print(f"FAILED: {len(FAILURES)} 项 — {FAILURES}")
        return 1
    print("ALL PASSED")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
