"""冒烟测试：确认原生八维主判真的跑起来了。

为什么需要它：`analyze_case` 里主判包在 try/except 里，任何异常
（实测是一次误删常量导致的 NameError）都会被吞掉，表面上流水线正常跑完、
每个维度都定档「无问题」，离线指标只表现为召回突然归零。
这种静默失效比崩溃危险得多，所以用一条 case 做前置断言。
"""

from __future__ import annotations

import sys

CASE = "I2VA_主体时序一致性_30条/judge_subject_011"


def main() -> int:
    import aigc_eval.judge.pipeline as jp

    status = "未被调用"
    original = jp.GeminiZoomInspector.scan_unified

    def inner(self, *args, **kwargs):
        nonlocal status
        try:
            out = original(self, *args, **kwargs)
        except Exception as err:  # noqa: BLE001 - 冒烟测试要看见异常
            status = f"抛异常 {type(err).__name__}: {err}"
            raise
        status = "OK" if isinstance(out, dict) and out else f"返回 {type(out).__name__}"
        return out

    jp.GeminiZoomInspector.scan_unified = inner

    report = jp.analyze_case(
        CASE, use_vlm=True, provider="agentk", source="kling", refresh=True, allow_fallback=False
    )

    bad = []
    print(f"  scan_unified           {status}")
    if status != "OK":
        bad.append(f"scan_unified: {status}")
    # 主判的产物必须一路落到报告里：scan_unified 返回了 dict 但报告里的八维是空的，
    # 说明中间的收编环节吞了结果，症状与主判自己失败完全一样。
    dims = report.get("rubric")
    ok = isinstance(dims, list) and bool(dims)
    print(f"  report['rubric']       {'OK' if ok else '空'}")
    if not ok:
        bad.append("report[rubric] 为空")
    print("  analysis_status", report.get("analysis_status"))
    if bad:
        print("\n失败：\n  - " + "\n  - ".join(bad))
        return 1
    print("\n通过：原生八维主判有产出")
    return 0


if __name__ == "__main__":
    sys.exit(main())
