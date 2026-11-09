"""不调模型的离线替身，`use_vlm=False` 时接管观察器与验证器。

它**不伪造任何维度结论**：八维子判据一律返回 `not_observable`，issues 为空。
离线路径没看过视频，给出 pass 或 violated 都是凭空捏造，而 native8 的报告没有
综合分来稀释单条结论，一条捏造的 pass 会直接呈现为「这一项已核验通过」。所以
离线模式的语义只能是「本次未作判断」，由确定性聚合把它落成待复核。

因此它的用途也就限定在编排回归：抽帧、聚合、硬门控、成本、报告渲染这些确定性
环节可以在没有网络和额度的前提下被端到端跑通并逐字段比对，而真实模型有随机性，
做不到这一点。
"""
from __future__ import annotations

from typing import Any

from aigc_eval.judge.native8 import DIMENSIONS, SCHEMA_VERSION


class MockVLMObserver:
    def observe(self, profile: str, metadata: dict[str, Any], mode: str) -> dict[str, Any]:
        del profile, metadata, mode
        return {
            "observations": [{
                "time": "全片", "fact": "离线模式不执行视频事实判断。",
                "source": "mock_observer", "confidence": "low", "tags": ["integrity"],
            }],
            "audio_facts": {},
            "reference_facts": [],
            "focus_intervals": [],
            "_video_mode": "frames",
        }


class MockVLMVerifier:
    def verify(self, profile: str) -> dict[str, Any]:
        del profile
        return {
            "schema_version": SCHEMA_VERSION,
            "issues": [],
            "rubric": [
                {
                    "dimension_id": dimension["id"],
                    "subcriteria": [
                        {"key": key, "verdict": "not_observable", "issue_ids": []}
                        for key, _name in dimension["subcriteria"]
                    ],
                }
                for dimension in DIMENSIONS
            ],
        }
