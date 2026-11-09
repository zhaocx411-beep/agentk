"""容器内诊断脚本的公共前置：把平台身份借到当前进程。

## 为什么需要这一步

容器云只把 ``KWS_SERVICE_CREDENTIAL`` 注入到 **START_COMMAND 拉起的服务进程**里。
``kcsize``（PID 1）没有它，WebConsole 另起的 shell 也没有。实测（prod，2026-09-03）：

* pid 1 ``kcsize api``            → 无凭证
* pid 25 ``python3 worker.py``    → 有凭证
* pid 26 ``python3 -m aigc_eval`` → 有凭证
* WebConsole 的 bash             → 无凭证

所以在 WebConsole 里手工跑的任何诊断，若不先借身份，一律得到 ``unauthorized``——
而它和"服务名真的没授权"的报错**完全一样**。我们为此误判过一轮，以为是平台侧没加白。

这个模块存在的唯一理由就是让那次误判不再发生：诊断脚本一律先调 ``borrow_identity()``，
拿不到就明确说"没找到带凭证的进程"，而不是让调用去撞一个语义含混的 unauthorized。
"""

from __future__ import annotations

import os
from pathlib import Path

# 只借这两族。KWS_* 是平台身份，AIGC_EVAL_* 是我们自己的业务参数（对象存储），
# 两者都得跟服务进程一致，否则诊断验的不是服务实际在用的配置。
_BORROW_PREFIXES = ("KWS_", "AIGC_EVAL_")


def _read_environ(pid: str) -> dict[str, str]:
    try:
        raw = Path(f"/proc/{pid}/environ").read_text()
    except OSError:
        return {}
    return dict(kv.split("=", 1) for kv in raw.split("\0") if "=" in kv)


def borrow_identity() -> tuple[str, dict[str, str]] | None:
    """找到带凭证的服务进程，把它的身份与业务参数灌进 ``os.environ``。

    返回 ``(pid, 借到的变量)``，找不到返回 None。

    必须在**建 AgentK 客户端之前**调用：SDK 的 ``identity_headers()`` 是在建客户端
    时读环境生成 ``kws-bin`` / ``kwsCredential`` 头的，之后再改环境不会生效。
    """
    if not Path("/proc").is_dir():
        return None
    for pid in sorted((p for p in os.listdir("/proc") if p.isdigit()), key=int):
        env = _read_environ(pid)
        if not env.get("KWS_SERVICE_CREDENTIAL"):
            continue
        borrowed = {key: value for key, value in env.items() if key.startswith(_BORROW_PREFIXES)}
        os.environ.update(borrowed)
        return pid, borrowed
    return None


def announce_identity() -> bool:
    """借身份并打印结果。返回是否借到。

    借不到时不直接退出：本机（没有 /proc 或没在容器里）也可能拿现成的环境变量跑，
    让脚本继续走下去，由后面真实调用去判定，比在这里一刀切更有用。
    """
    if os.environ.get("KWS_SERVICE_CREDENTIAL"):
        print("  身份     当前进程已带凭证，无需借用")
        return True
    borrowed = borrow_identity()
    if borrowed is None:
        print("  身份     没找到带凭证的进程（本机运行属正常；容器里请先确认服务在跑）")
        return False
    pid, values = borrowed
    print(
        f"  身份     借用 pid {pid} 的环境"
        f"（{len(values)} 个变量，凭证 {len(values['KWS_SERVICE_CREDENTIAL'])} 字节）"
    )
    return True
