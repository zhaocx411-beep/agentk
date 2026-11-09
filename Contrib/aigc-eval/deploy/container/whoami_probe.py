"""身份探针：确认平台认不认我们，以及该走哪条传输通道。

``WhoAmI`` 只回显"平台认到你是谁"，**不需要任何能力权限、也不产生模型开销**，
所以可以反复跑。冒烟脚本要付费调模型，不适合拿来轮询。

## 它同时回答两个问题

1. **身份认不认。** 回出 display_name 就说明 KSN 已在这个环境登记并绑好 Service/App。
2. **哪条通道能用。** 两条通道都试，因为它们的开放范围不同：生产环境的 HTTP 固定
   网关（``agentk-api.internal``）不对我们开放，只有 KESS gRPC 通；staging 反过来
   HTTP 就够，且开发机上没有 kess-agent 时只有 HTTP 可用。

第 2 点是这个脚本存在的主要理由。两条通道失败时的错误**一模一样**（都是
``unauthorized``），与"身份没登记"无法区分——我们照着单条通道的报错去追平台侧加白，
白等了一轮，直到把两条并排跑才看出是通道问题。并排跑一次就能定性，所以别省这一步。

跑法（容器内）：

    python3 /home/web_server/aigc-eval/bin/whoami_probe.py

配置里选定的那条通道通了就退出码 0，否则 1。
"""

from __future__ import annotations

import asyncio
import os
import sys

from container_env import announce_identity

from aigc_eval.config import load_agentk_config


def main() -> int:
    print("[环境]")
    announce_identity()
    # 借完身份才读配置：两者都来自环境变量。
    config = load_agentk_config()
    print(f"  服务名   {os.environ.get('KWS_SERVICE_NAME') or '(未注入)'}")
    print(f"  环境     {os.environ.get('KWS_SERVICE_STAGE') or '(未注入)'}")
    print(f"  选定通道 {config.transport}（http 时连 {config.base_url}）")

    from agentk.api.v1.service_pb import WhoAmIRequest
    from agentk.client import Options
    from agentk.client.grpc import create_client as create_grpc
    from agentk.client.http import create_client as create_http

    print("\n[两条通道并排]")
    results: dict[str, bool] = {}
    for kind, factory, options in (
        ("http", create_http, Options(endpoint=config.base_url)),
        # endpoint 必须留空才会触发 KESS 发现；给了地址就退回直连覆盖。
        ("grpc", create_grpc, Options()),
    ):
        try:
            identity = asyncio.run(factory(options).who_am_i(WhoAmIRequest()))
        except Exception as error:  # noqa: BLE001 - 诊断脚本要把失败打成一行
            print(f"  {kind:5} FAIL {type(error).__name__}: {error}")
            results[kind] = False
        else:
            print(f"  {kind:5} OK   {identity}")
            results[kind] = True

    print("\n[结论]")
    if results.get(config.transport):
        print(f"  选定的 {config.transport} 通道可用，身份这一层通了。")
        print("  接下来跑 smoke_check.py 验模型调用与网关取素材。")
        return 0

    working = [kind for kind, ok in results.items() if ok]
    if working:
        # 这正是 prod 上踩到的那一种：身份没问题，是通道选错了。
        print(
            f"  身份是好的（{'、'.join(working)} 通道能回身份），但配置选的是 {config.transport}，那条不通。"
        )
        print("  改 config.default_agentk_transport 的选择规则，别去找平台加白。")
        return 1

    print("  两条通道都拒。这才是身份本身的问题：KSN 没在这个环境登记，")
    print("  或者只登记了 KSN、没完成 Service/App 绑定。找 AgentK 平台维护方。")
    return 1


if __name__ == "__main__":
    sys.exit(main())
