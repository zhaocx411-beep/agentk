"""金标集的 native8-v1 判定 vs 人工标注一致性分析。

**比的是什么**：每个 case 的八个维度各一档（`0` 恶劣 / `0.5` 瑕疵 / `1` 无问题），
AI 侧取 `rubric[].score`（`finalize_native8_result` 确定性算出的档位），人工侧取
标注文件里同名维度的 `s` 字段。一个 (case, 维度) 构成一个可比对样本。

**为什么不比子判据**：native8 有 28 条子判据，但人工标注是在 v41 时期按八维打的，
一格里只有一个档位加一段自由文本理由，没有子判据字段（`adjudication_overlay.json`
的复核层同样只有 `case_id` + `dimension`）。把八维的一个档位摊回 28 条子判据需要
凭空指定「这一档是哪条子判据造成的」，那是编数据。所以子判据只做**单侧统计**
（verdict 分布，见 `verdict_counts`），不参与一致率。

**not_observable 怎么算**：`finalize_native8_result` 对「没有任何 violated」的维度
一律给 1.0，无论那是「查过没问题」还是「压根没查」。直接拿它比对会把「没评过」
记成一致，正是这个 harness 该抓的失真。所以：

- 维度 tier 为 1.0 **且**存在未决子判据（`unresolved_subcriteria` 非空）时，判为
  `not_assessed`，**从一致率分母里剔除**，单独成桶上报（`not_assessed` 段），并
  统计其中人工认为有问题的条数 —— 那是被口径隐藏起来的疑似漏报；
- tier 为 0 / 0.5 的维度即使还有未决子判据也照常参与比对：这个档位是由某条已
  校验通过的 violated 立起来的，不依赖「没发现问题」这个前提；
- `applicable=False` 的维度没有 AI 结论可比，同样剔除。

选剔除而不是「按 pass 计」或「按不一致计」，是因为后两者都会把「协议覆盖不足」
和「判断不准」混进同一个数字里。分开报，才能分别定位。

映射键：case 目录 source.json 的 work_id == 标注文件的顶层 key。

用法：
    ./deploy/run_offline.sh -m aigc_eval.batch.eval_vs_human
    ./deploy/run_offline.sh -m aigc_eval.batch.eval_vs_human --json out.json
"""

from __future__ import annotations

import argparse
import collections
import json
import pathlib
import re
from typing import Any

from aigc_eval import paths
from aigc_eval.judge.native8 import DIMENSION_NAMES as ANNOTATION_DIMS, SCHEMA_VERSION
from aigc_eval.judge.settings import CACHE_VERSION

# 参与一致性分析的数据集。native8-v1 对 T2V/I2V/V2V 使用同一八维协议。
# 注意：目录不存在时 `work_id_map` 的 glob 自然为空，不会报错 —— 数据集没同步到
# 本机时脚本仍可跑，只是少了那一部分样本。
COLLECTIONS = ("T2VA_gold50_AI", "I2VA_gold50_AI", "V2VA_gold50_AI")
TIERS = ("0", "0.5", "1")
TIER_NAME = {"0": "恶劣", "0.5": "瑕疵", "1": "无问题"}

# 维度在一致率里的三种去向。名字进 JSON 明细，改名等于改对外口径。
COMPARABLE = "comparable"
NOT_ASSESSED = "not_assessed"
NOT_APPLICABLE = "not_applicable"

VERDICTS = ("pass", "violated", "not_observable", "not_applicable")


def load_human(path: pathlib.Path) -> dict[str, dict[str, str]]:
    """work_id -> {维度: 得分字符串}。空串视为未标注，直接丢弃。"""
    raw = json.loads(path.read_text(encoding="utf-8"))
    out: dict[str, dict[str, str]] = {}
    for work_id, payload in raw.items():
        dims: dict[str, str] = {}
        for dim, cell in (payload.get("m") or {}).items():
            score = str((cell or {}).get("s") or "").strip()
            if score in TIERS:
                dims[dim] = score
        if dims:
            out[str(work_id)] = dims
    return out


def work_id_map() -> dict[str, str]:
    """work_id -> "<collection>/<case>"。"""
    mapping: dict[str, str] = {}
    for collection in COLLECTIONS:
        base = paths.WORKSPACE_ROOT / collection
        for source in sorted(base.glob("*/source.json")):
            try:
                work_id = str(json.loads(source.read_text(encoding="utf-8")).get("work_id") or "")
            except (json.JSONDecodeError, OSError):
                continue
            if work_id:
                mapping[work_id] = f"{collection}/{source.parent.name}"
    return mapping


def find_result(case_id: str, engine: str = "gemini") -> pathlib.Path | None:
    """定位某个 case 在指定引擎下的结果缓存。

    缓存名形如 ``<collection>_<case>_<hash6>__kling__<engine>__auto__<版本>.json``。
    自从加了 KLink OpenAI 兼容层，同一个 case 会同时存在 ``__gemini__``（Vertex
    直连）与 ``__gemini-openai__`` 两份结果 —— 只按前缀取 ``sorted()[0]`` 会静默
    落到字典序更小的 ``gemini-openai`` 上，导致「换了标注文件重跑」时同时换掉了
    AI 侧数据，两次结果不可比。所以这里必须按引擎精确匹配。

    只收当前 `CACHE_VERSION` 的缓存：更早的版本是 v41 五维报告，字段形状不同，
    读进来会静默缺 rubric。
    """
    prefix = case_id.replace("/", "_") + "_"
    marker = f"__{engine}__"
    hits = [
        path
        for path in paths.RESULTS_ROOT.glob("*.json")
        if path.name.startswith(prefix)
        and marker in path.name
        and path.name.endswith(f"__{CACHE_VERSION}.json")
    ]
    if not hits:
        return None

    def cache_order(path: pathlib.Path) -> tuple[int, float]:
        match = re.search(r"__v(\d+)\.json$", path.name)
        version = int(match.group(1)) if match else -1
        return version, path.stat().st_mtime

    return max(hits, key=cache_order)


def load_report(case_id: str, engine: str = "gemini") -> dict[str, Any] | None:
    """读取并确认协议版本；非 native8-v1 的缓存直接当未命中。"""
    path = find_result(case_id, engine)
    if path is None:
        return None
    report = json.loads(path.read_text(encoding="utf-8"))
    return report if report.get("schema_version") == SCHEMA_VERSION else None


def dimension_states(report: dict[str, Any]) -> dict[str, dict[str, Any]]:
    """维度名 -> {tier, status, unresolved}。

    `status` 决定该维度是否进一致率分母，判据见模块 docstring：tier 1.0 且尚有
    未决子判据的维度是「没评过」而非「没问题」，必须与真正判过的 1.0 分开。
    """
    out: dict[str, dict[str, Any]] = {}
    for row in report.get("rubric") or []:
        if not isinstance(row, dict):
            continue
        name = str(row.get("dimension_name") or "")
        unresolved = [str(key) for key in (row.get("unresolved_subcriteria") or [])]
        score = row.get("score")
        if not row.get("applicable") or not isinstance(score, (int, float)):
            out[name] = {"tier": None, "status": NOT_APPLICABLE, "unresolved": unresolved}
            continue
        tier = "1" if float(score) >= 1 else "0" if float(score) <= 0 else "0.5"
        status = NOT_ASSESSED if tier == "1" and unresolved else COMPARABLE
        out[name] = {"tier": tier, "status": status, "unresolved": unresolved}
    return out


def ai_scores(case_id: str, engine: str = "gemini") -> dict[str, str] | None:
    """可比对维度的档位（纯离线，零模型成本）。

    只返回 `status == COMPARABLE` 的维度 —— 调用方直接拿它当分母，掺进「没评过」
    的维度就会虚高。要看完整八维状态请用 `ai_dimension_states`。
    """
    states = ai_dimension_states(case_id, engine)
    if states is None:
        return None
    return {
        name: str(state["tier"])
        for name, state in states.items()
        if state["status"] == COMPARABLE
    }


def ai_dimension_states(case_id: str, engine: str = "gemini") -> dict[str, dict[str, Any]] | None:
    """八维完整状态，含被剔除的维度及其未决子判据。"""
    report = load_report(case_id, engine)
    return None if report is None else dimension_states(report)


def verdict_counts(reports: list[dict[str, Any]]) -> dict[str, int]:
    """28 条子判据的 verdict 分布（单侧统计，人工侧无对应粒度可比）。

    `not_observable` 的绝对量就是协议覆盖率的体检指标：它高，说明大量子判据没
    落地结论，此时八维一致率再高也只是在少数被真正评过的维度上成立。
    """
    counter: collections.Counter[str] = collections.Counter()
    for report in reports:
        for row in report.get("rubric") or []:
            for criterion in (row.get("subcriteria") or []) if isinstance(row, dict) else []:
                if isinstance(criterion, dict):
                    counter[str(criterion.get("verdict") or "not_observable")] += 1
    return {verdict: counter[verdict] for verdict in VERDICTS}


def prf(tp: int, fp: int, fn: int) -> tuple[float, float, float]:
    precision = tp / (tp + fp) if tp + fp else 0.0
    recall = tp / (tp + fn) if tp + fn else 0.0
    f1 = 2 * precision * recall / (precision + recall) if precision + recall else 0.0
    return precision, recall, f1


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--human", default="/Users/fscnb/Downloads/金标考试_gold_label.json")
    parser.add_argument("--json", default="", help="把明细写到文件")
    parser.add_argument(
        "--engine", default="gemini",
        help="用哪套结果缓存：gemini=Vertex 直连，gemini-openai=KLink OpenAI 兼容层",
    )
    args = parser.parse_args()

    human = load_human(pathlib.Path(args.human))
    id_map = work_id_map()

    pairs: list[tuple[str, str, str, str]] = []  # (case, dim, human, ai)
    excluded: list[tuple[str, str, str, str]] = []  # (case, dim, human, 剔除原因)
    reports: list[dict[str, Any]] = []
    skipped: list[str] = []
    cases: set[str] = set()
    for work_id, human_dims in human.items():
        case_id = id_map.get(work_id)
        if not case_id:
            continue
        report = load_report(case_id, args.engine)
        if report is None:
            skipped.append(case_id)
            continue
        reports.append(report)
        cases.add(case_id)
        states = dimension_states(report)
        for dim, human_score in human_dims.items():
            state = states.get(dim)
            if state is None:
                continue
            if state["status"] == COMPARABLE:
                pairs.append((case_id, dim, human_score, str(state["tier"])))
            else:
                excluded.append((case_id, dim, human_score, str(state["status"])))

    if not pairs:
        print("没有可比对的样本")
        return

    total = len(pairs)
    agree = sum(1 for _, _, h, a in pairs if h == a)
    print(f"样本：{len(cases)} case → {total} 个可比对维度对（八维中已判过的部分）")
    if skipped:
        print(f"跳过（无 {SCHEMA_VERSION} 结果缓存）：{len(skipped)}")
    print(f"\n{'='*66}\n【整体】\n{'='*66}")
    print(f"逐维度准确率 (exact match)   {agree}/{total} = {agree/total:.1%}")

    # 二分类视角：把「有问题」(0 或 0.5) 当正类 —— 质检最关心能否发现问题
    tp = sum(1 for _, _, h, a in pairs if h != "1" and a != "1")
    fp = sum(1 for _, _, h, a in pairs if h == "1" and a != "1")
    fn = sum(1 for _, _, h, a in pairs if h != "1" and a == "1")
    tn = sum(1 for _, _, h, a in pairs if h == "1" and a == "1")
    p, r, f1 = prf(tp, fp, fn)
    print(f"\n二分类「有问题 vs 无问题」：")
    print(f"  TP={tp}  FP={fp}  FN={fn}  TN={tn}")
    print(f"  Precision {p:.1%} · Recall {r:.1%} · F1 {f1:.3f}")
    print(f"  Accuracy  {(tp+tn)/total:.1%}")

    # Cohen's kappa（三分类，排除偶然一致）
    n = total
    po = agree / n
    hc = collections.Counter(h for _, _, h, _ in pairs)
    ac = collections.Counter(a for _, _, _, a in pairs)
    pe = sum(hc[t] / n * ac[t] / n for t in TIERS)
    kappa = (po - pe) / (1 - pe) if pe < 1 else 0.0
    print(f"\nCohen's kappa {kappa:.3f}  (>0.6 好 · >0.8 很好)")

    # 剔除桶必须和一致率一起看：分母缩小本身可能就是问题所在。
    not_assessed = [row for row in excluded if row[3] == NOT_ASSESSED]
    hidden = [row for row in not_assessed if row[2] != "1"]
    print(f"\n{'='*66}\n【剔除桶】\n{'='*66}")
    print(f"未评估（tier=1 但子判据未决，不计入分母）  {len(not_assessed)}")
    print(f"  其中人工认为有问题（疑似被口径隐藏的漏报）  {len(hidden)}")
    if not_assessed:
        by_dim = collections.Counter(dim for _, dim, _, _ in not_assessed)
        print("  分布：" + "、".join(f"{dim} {cnt}" for dim, cnt in by_dim.most_common()))
    print(f"不适用（applicable=False）  {sum(1 for row in excluded if row[3] == NOT_APPLICABLE)}")

    verdicts = verdict_counts(reports)
    observed = sum(verdicts[key] for key in ("pass", "violated"))
    coverage = observed / max(1, observed + verdicts["not_observable"])
    print(f"\n{'='*66}\n【子判据 verdict 分布（单侧）】\n{'='*66}")
    print("  " + " · ".join(f"{key} {verdicts[key]}" for key in VERDICTS))
    print(f"  已给出结论占比（排除 not_applicable）  {coverage:.1%}")

    print(f"\n{'='*66}\n【按维度】\n{'='*66}")
    print(f"{'维度':<16}{'n':>4}{'准确率':>9}{'P':>8}{'R':>8}{'F1':>8}{'未评估':>8}")
    per_dim: dict[str, Any] = {}
    for dim in ANNOTATION_DIMS:
        rows = [(h, a) for _, d, h, a in pairs if d == dim]
        if not rows:
            continue
        skipped_dim = sum(1 for _, d, _, why in excluded if d == dim and why == NOT_ASSESSED)
        acc = sum(1 for h, a in rows if h == a) / len(rows)
        dtp = sum(1 for h, a in rows if h != "1" and a != "1")
        dfp = sum(1 for h, a in rows if h == "1" and a != "1")
        dfn = sum(1 for h, a in rows if h != "1" and a == "1")
        dp, dr, df1 = prf(dtp, dfp, dfn)
        per_dim[dim] = {
            "n": len(rows), "accuracy": acc, "precision": dp, "recall": dr, "f1": df1,
            "not_assessed": skipped_dim,
        }
        print(f"{dim:<16}{len(rows):>4}{acc:>8.0%}{dp:>8.0%}{dr:>8.0%}{df1:>8.2f}{skipped_dim:>8}")

    print(f"\n{'='*66}\n【混淆矩阵】行=人工 列=AI\n{'='*66}")
    print(f"{'':>10}" + "".join(f"{TIER_NAME[t]:>10}" for t in TIERS))
    matrix = collections.Counter((h, a) for _, _, h, a in pairs)
    for ht in TIERS:
        row = f"{TIER_NAME[ht]:>10}"
        for at in TIERS:
            row += f"{matrix[(ht, at)]:>10}"
        print(row)

    print(f"\n{'='*66}\n【偏严 / 偏松】\n{'='*66}")
    order = {"0": 0, "0.5": 1, "1": 2}
    stricter = sum(1 for _, _, h, a in pairs if order[a] < order[h])
    looser = sum(1 for _, _, h, a in pairs if order[a] > order[h])
    print(f"AI 比人工严（扣更多）{stricter} ({stricter/total:.1%})")
    print(f"AI 比人工松（漏问题）{looser} ({looser/total:.1%})")

    print(f"\n{'='*66}\n【分歧最多的 case】\n{'='*66}")
    by_case = collections.Counter(c for c, _, h, a in pairs if h != a)
    for case_id, cnt in by_case.most_common(8):
        detail = [f"{d}:人{h}/AI{a}" for c, d, h, a in pairs if c == case_id and h != a]
        print(f"  {case_id:32} {cnt} 处  {' | '.join(detail[:3])}")

    if args.json:
        pathlib.Path(args.json).write_text(
            json.dumps(
                {
                    "schema_version": SCHEMA_VERSION,
                    "engine": args.engine,
                    "n_cases": len(cases),
                    "n_pairs": total,
                    "accuracy": agree / total,
                    "binary": {"tp": tp, "fp": fp, "fn": fn, "tn": tn, "precision": p, "recall": r, "f1": f1},
                    "kappa": kappa,
                    "per_dim": per_dim,
                    "not_assessed": {
                        "n": len(not_assessed),
                        "human_flagged": len(hidden),
                        "pairs": [
                            {"case": c, "dim": d, "human": h}
                            for c, d, h, _ in not_assessed
                        ],
                    },
                    "subcriteria_verdicts": verdicts,
                    "pairs": [{"case": c, "dim": d, "human": h, "ai": a} for c, d, h, a in pairs],
                },
                ensure_ascii=False,
                indent=2,
            ),
            encoding="utf-8",
        )
        print(f"\n明细 → {args.json}")


if __name__ == "__main__":
    main()
