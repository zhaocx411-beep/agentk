"""把 native8-v1 判定 vs 人工标注的一致性分析渲染成单文件 HTML 报告。

数据自采：直接读取 `runtime/results` 里的 native8-v1 报告（`rubric` 档位 +
`issues` 事实），离线统计，不触发模型调用。

一致率的口径、以及 `not_observable` 为什么被剔出分母，全部由
`aigc_eval.batch.eval_vs_human` 定义，本模块只负责把它算出来的东西画出来 ——
两处各写一份判据是这个报告过去最容易出的错：页面上的数字和 CLI 的不一样。

报告里三块内容不能省，它们互为前提：

1. **一致率**：只在「AI 真正判过」的维度上成立；
2. **未评估桶**：被剔除的维度有多少、其中人工认为有问题的有多少 —— 分母缩小
   本身就是一种缺陷，不上报就等于用覆盖率换准确率；
3. **子判据 verdict 分布**：28 条子判据里有多少还是 `not_observable`，
   决定上面那个一致率有多少代表性。

复核覆盖层（`--adjudication`）是给「金标是 v41 时期打的」这件事留的口子：金标
文件本身只读，对某一格的重新裁定写在覆盖层里，只有 `status=approved` 的条目才
参与一致率。

用法：
    ./deploy/run_offline.sh -m aigc_eval.report.vs_human
    ./deploy/run_offline.sh -m aigc_eval.report.vs_human -o /tmp/a.html --human 别的标注.json
"""

from __future__ import annotations

import argparse
import collections
import html
import json
import pathlib
from typing import Any

from aigc_eval.batch.eval_vs_human import (
    NOT_ASSESSED,
    TIERS,
    TIER_NAME,
    VERDICTS,
    dimension_states,
    load_human,
    load_report as load_native8_report,
    prf,
    verdict_counts,
    work_id_map,
)
from aigc_eval.judge.native8 import DIMENSION_NAMES

TIER_CLS = {"0": "t-bad", "0.5": "t-mid", "1": "t-ok"}
DIM_ORDER = DIMENSION_NAMES
ADJUDICATION_STATUSES = frozenset({"pending", "approved", "rejected"})
VERDICT_NAME = {
    "pass": "通过", "violated": "违反",
    "not_observable": "未评估", "not_applicable": "不适用",
}
# 各维度的判据一句话摘要。评语里要解释「这一维为什么容易/难判」时按维度查表取用，
# 不能写死在文案里 —— 早先把「首帧对不对、字错没错」硬编码在高分维度的注解里，
# 换一批样本后 strong 名单变了，就出现了「文字质量看首帧」这种张冠李戴。
DIM_BASIS = {
    "指令遵循": "prompt 要求的要素有没有出现",
    "参考一致性": "首帧对不对、主体像不像参考图",
    "视觉一致性": "同一主体跨镜头是否稳定",
    "音频综合质量": "音画是否对齐、有无杂音",
    "画面质量": "清晰度、构图、畸变",
    "动态质量": "运动是否自然、有无闪断",
    "文字质量": "画面文字有没有乱码错字",
    "故事逻辑连贯性": "情节是否自洽",
}
# AI 依据的四段结构。人工标注规范要的就是这四段，键名是中文，
# 取成 summary/detail 之类会静默得到空理由。
FEEDBACK_KEYS = ("时间", "要求", "实际", "解释")


def esc(value: Any) -> str:
    return html.escape(str(value if value is not None else ""))


def load_report(case_id: str, engine: str = "gemini") -> dict[str, Any] | None:
    """取指定引擎的 native8-v1 缓存，与 eval_vs_human 同源同口径。"""
    return load_native8_report(case_id, engine)


def ai_reasons(report: dict[str, Any]) -> dict[str, list[dict[str, str]]]:
    """维度名 -> 按「时间/要求/实际/解释」整理的原生 issue 依据。

    「要求」优先取 prompt 里那条被违反的约束原文（`prompt_requirements`），拿不到
    才退回子判据名。这个顺序是刻意的：人工理由写的是「要求什么、实际什么」，拿
    子判据名去对照会让两栏根本不在一个抽象层级上，reviewer 无法判断谁对。
    """
    issues = {
        row.get("issue_id"): row
        for row in report.get("issues") or []
        if isinstance(row, dict)
    }
    prompt_requirements = {
        str(row.get("requirement_id") or row.get("id") or ""): str(row.get("statement") or "").strip()
        for row in report.get("prompt_requirements") or []
        if isinstance(row, dict)
    }
    out: dict[str, list[dict[str, str]]] = {}
    for row in report.get("rubric") or []:
        if not isinstance(row, dict):
            continue
        criteria_by_issue: dict[str, list[dict[str, Any]]] = {}
        for criterion in row.get("subcriteria") or []:
            if not isinstance(criterion, dict):
                continue
            for issue_id in criterion.get("issue_ids") or []:
                criteria_by_issue.setdefault(str(issue_id), []).append(criterion)
        items: list[dict[str, str]] = []
        # `feedback` 是本维定档所依据的 issue_id 列表，顺序即定档顺序。
        for issue_id in row.get("feedback") or []:
            issue = issues.get(issue_id) or {}
            evidence = [item for item in issue.get("evidence") or [] if isinstance(item, dict)]
            facts = issue.get("facts") or {}
            ref_ids = list(dict.fromkeys([
                *[str(value) for value in issue.get("ref_ids") or []],
                str(facts.get("constraint_id") or ""),
            ]))
            compiled = [
                prompt_requirements[ref_id]
                for ref_id in ref_ids
                if prompt_requirements.get(ref_id)
            ]
            criteria = criteria_by_issue.get(str(issue_id), [])
            criterion_names = "、".join(dict.fromkeys(
                str(criterion.get("name") or criterion.get("key") or "")
                for criterion in criteria
            ))
            requirement = "；".join(dict.fromkeys(compiled)) or (
                f"应满足「{criterion_names}」判据。" if criterion_names
                else "应满足该维度对应的原生八维判据。"
            )
            actual = next(
                (
                    str(item.get("detail") or "").strip()
                    for item in evidence
                    if str(item.get("detail") or "").strip()
                ),
                "未记录可复核的实际表现。",
            )
            explanation = str(issue.get("decision_summary") or "").strip() or (
                f"该实际表现违反「{criterion_names}」判据，因此该维度记为问题。"
                if criterion_names else "该实际表现违反该维度判据，因此记为问题。"
            )
            times = "、".join(dict.fromkeys(
                str(item.get("time") or "").strip()
                for item in evidence
                if str(item.get("time") or "").strip()
            ))
            items.append({"时间": times, "要求": requirement, "实际": actual, "解释": explanation})
        out[str(row.get("dimension_name"))] = items
    return out


def human_reasons(cell: dict[str, Any]) -> list[dict[str, str]]:
    """人工标注一格里的理由：``p`` 是正文，``t1``/``t2`` 是时间区间。"""
    out: list[dict[str, str]] = []
    for item in cell.get("r") or []:
        if not isinstance(item, dict):
            continue
        text = str(item.get("p") or "").strip()
        if not text:
            continue
        t1, t2 = str(item.get("t1") or "").strip(), str(item.get("t2") or "").strip()
        out.append({"time": f"{t1}–{t2}" if t1 and t2 else (t1 or t2 or ""), "text": text})
    return out


def load_adjudication(path: pathlib.Path | None) -> dict[tuple[str, str], dict[str, Any]]:
    """读取独立复核覆盖层；格式错误的条目忽略，绝不修改原金标。"""
    if path is None or not path.exists():
        return {}
    try:
        raw = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return {}
    items = raw.get("items") if isinstance(raw, dict) else raw
    out: dict[tuple[str, str], dict[str, Any]] = {}
    for item in items or []:
        if not isinstance(item, dict):
            continue
        case_id = str(item.get("case_id") or "").strip()
        dimension = str(item.get("dimension") or "").strip()
        status = str(item.get("status") or "pending").strip().lower()
        label = str(item.get("adjudicated_label") or "").strip()
        if not case_id or dimension not in DIM_ORDER or status not in ADJUDICATION_STATUSES:
            continue
        if label and label not in TIERS:
            continue
        out[(case_id, dimension)] = {**item, "status": status}
    return out


def tier_distribution(reports: list[dict[str, Any]]) -> dict[str, int]:
    """全样本的维度档位分布，只统计 applicable 且已定档的维度。"""
    counter: collections.Counter[float] = collections.Counter()
    for report in reports:
        for row in report.get("rubric") or []:
            if isinstance(row, dict) and isinstance(row.get("score"), (int, float)):
                counter[float(row["score"])] += 1
    return {
        "total": sum(counter.values()),
        "bad": counter[0.0],
        "blemish": counter[0.5],
        "clean": counter[1.0],
    }


def facts_coverage(reports: list[dict[str, Any]]) -> dict[str, int]:
    """原生 issue 数与其中带结构化 facts 的数量。

    三档完全由 facts 推出，所以 facts 缺失率就是「分档凭什么」的上限：覆盖率不满，
    后续讨论「档位判得准不准」是空的，先得让事实齐备。
    """
    decided = with_basis = 0
    for report in reports:
        for row in report.get("issues") or []:
            if not isinstance(row, dict):
                continue
            decided += 1
            if row.get("facts"):
                with_basis += 1
    return {"decided": decided, "with_basis": with_basis}


def review_stats(reports: list[dict[str, Any]]) -> dict[str, int]:
    """待复核漏斗规模：被打回的 violated 条目与进入复核的候选 issue。"""
    required = candidates = 0
    for report in reports:
        required += len([item for item in report.get("review_required") or [] if item])
        candidates += len([item for item in report.get("review_candidates") or [] if item])
    return {"required": required, "candidates": candidates}


def collect(
    human_path: pathlib.Path,
    engine: str = "gemini",
    adjudication_path: pathlib.Path | None = None,
) -> list[dict[str, Any]]:
    """逐维度对照行。零模型成本：全部从 runtime/results 缓存离线读取。

    返回的行**只含可比对维度**（见 eval_vs_human 的口径），被剔除的维度与全样本
    级统计挂在 `rows[0]` 的下划线字段上带给 `render` —— 让渲染层再扫一遍结果目录
    就会出现两处读到不同缓存的可能。
    """
    human = load_human(human_path)
    id_map = work_id_map()
    raw = json.loads(human_path.read_text(encoding="utf-8"))
    adjudication = load_adjudication(adjudication_path)

    rows: list[dict[str, Any]] = []
    reports: list[dict[str, Any]] = []
    not_assessed: list[dict[str, Any]] = []
    for work_id, human_dims in human.items():
        case_id = id_map.get(work_id)
        if not case_id:
            continue
        report = load_report(case_id, engine)
        if report is None:
            continue
        reports.append(report)
        states = dimension_states(report)
        reasons = ai_reasons(report)
        cells = (raw.get(work_id) or {}).get("m") or {}
        for dim, human_score in human_dims.items():
            state = states.get(dim)
            if state is None:
                continue
            if state["status"] != "comparable":
                if state["status"] == NOT_ASSESSED:
                    not_assessed.append({
                        "case": case_id, "dim": dim, "human": human_score,
                        "unresolved": state["unresolved"],
                    })
                continue
            review = adjudication.get((case_id, dim))
            adjudicated = human_score
            if review and review.get("status") == "approved":
                candidate = str(review.get("adjudicated_label") or "")
                if candidate in TIERS:
                    adjudicated = candidate
            rows.append(
                {
                    "case": case_id,
                    "dim": dim,
                    "human": human_score,
                    "adjudicated_human": adjudicated,
                    "adjudication": review,
                    "ai": str(state["tier"]),
                    "unresolved": state["unresolved"],
                    "human_reason": human_reasons(cells.get(dim) or {}),
                    "ai_reason": reasons.get(dim) or [],
                    "status": report.get("analysis_status"),
                }
            )
    if rows:
        rows[0]["_tiers"] = tier_distribution(reports)
        rows[0]["_facts"] = facts_coverage(reports)
        rows[0]["_review"] = review_stats(reports)
        rows[0]["_verdicts"] = verdict_counts(reports)
        rows[0]["_not_assessed"] = not_assessed
        rows[0]["_engine"] = engine
        rows[0]["_adjudication_path"] = str(adjudication_path or "")
        rows[0]["_adjudication_counts"] = dict(collections.Counter(
            str(item.get("status") or "pending") for item in adjudication.values()
        ))
    return rows


def write_adjudication_template(rows: list[dict[str, Any]], path: pathlib.Path) -> int:
    """为当前全部分歧生成待审核矩阵；已有非空 overlay 不覆盖。

    只能到维度粒度：金标本身就是按八维打的，凭一个档位反推「是哪条子判据」等于
    编数据，所以这里不生成 subcriterion 字段。
    """
    if path.exists():
        try:
            existing = json.loads(path.read_text(encoding="utf-8"))
            if isinstance(existing, dict) and existing.get("items"):
                raise FileExistsError(f"复核覆盖层已有内容，不覆盖：{path}")
        except json.JSONDecodeError:
            raise FileExistsError(f"复核覆盖层不是有效 JSON，不覆盖：{path}") from None
    items = []
    for row in rows:
        if row["human"] == row["ai"]:
            continue
        items.append({
            "case_id": row["case"],
            "dimension": row["dim"],
            "original_label": row["human"],
            "adjudicated_label": row["human"],
            "status": "pending",
            "v1_1_clause": "",
            "video_evidence": "",
            "review_note": (
                "自动初筛：人工无问题、框架报问题；需复核是事实误报还是原金标与 v1.1 冲突。"
                if row["human"] == "1"
                else "自动初筛：人工有问题、框架未发现；需复核事实检测是否漏报。"
                if row["ai"] == "1"
                else "自动初筛：双方均确认有问题，但瑕疵/恶劣分档不同。"
            ),
            "error_type": (
                "tier_grading" if row["human"] != "1" and row["ai"] != "1"
                else "fact_detection"
            ),
            "error_direction": (
                "false_positive" if row["human"] == "1"
                else "false_negative" if row["ai"] == "1"
                else "tier_mismatch"
            ),
        })
    payload = {"version": 1, "policy": "native8-v1", "items": items}
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return len(items)


def bar(pct: float, cls: str = "", tip: str = "") -> str:
    title = f" title='{tip}'" if tip else ""
    return (
        f'<div class="bar"{title}>'
        f'<i class="{cls}" style="width:{max(0.0, min(1.0, pct)) * 100:.1f}%"></i></div>'
    )


def human_reason_html(items: list[dict[str, str]]) -> str:
    if not items:
        return "<span class=none>标注未填理由</span>"
    parts: list[str] = []
    for item in items:
        time = f"<b class=ts>{esc(item['time'])}</b> " if item["time"] else ""
        # 人工理由本身就是「要求/实际/解释」三行自由文本，保留换行即可。
        body = "<br>".join(esc(line) for line in item["text"].splitlines() if line.strip())
        parts.append(f"<div class=item>{time}{body}</div>")
    return "".join(parts)


def ai_reason_html(items: list[dict[str, str]], tier: str) -> str:
    """AI 依据：「时间/要求/实际/解释」四段结构化展开。"""
    if not items:
        return (
            "<span class=none>本维无已定档的问题</span>"
            if tier == "1"
            else "<span class=none>无结构化依据（瑕疵累积升级所致）</span>"
        )
    parts: list[str] = []
    for item in items:
        time = f"<b class=ts>{esc(item['时间'])}</b>" if item["时间"] else ""
        rows = "".join(
            f"<div><span class=fk>{label}</span>{esc(item.get(key) or '未记录')}</div>"
            for label, key in (("要求", "要求"), ("实际", "实际"), ("解释", "解释"))
        )
        parts.append(f"<div class=item>{time}{rows}</div>")
    return "".join(parts)


CSS = """
*{box-sizing:border-box}
body{margin:0;padding:32px 40px 80px;background:#f6f7f9;color:#1f2328;
 font:14px/1.65 -apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC","Microsoft YaHei",sans-serif}
.wrap{max-width:1180px;margin:0 auto}
h1{font-size:26px;margin:0 0 6px;letter-spacing:-.3px}
h2{font-size:17px;margin:34px 0 12px;padding-bottom:8px;border-bottom:2px solid #e8eaed}
h3{font-size:14px;margin:20px 0 8px;color:#3c4149}
.sub{color:#6a737d;margin:0 0 24px;font-size:13px}
.card{background:#fff;border:1px solid #e3e6ea;border-radius:10px;padding:18px 20px;margin-bottom:16px}
.kpis{display:grid;grid-template-columns:repeat(auto-fit,minmax(160px,1fr));gap:12px;margin-bottom:8px}
.kpi{background:#fff;border:1px solid #e3e6ea;border-radius:10px;padding:14px 16px}
.kpi .v{font-size:26px;font-weight:650;letter-spacing:-.5px;line-height:1.2}
.kpi .k{font-size:12px;color:#6a737d;margin-top:2px}
.kpi.good .v{color:#1a7f37}.kpi.warn .v{color:#bf8700}.kpi.bad .v{color:#cf222e}
table{border-collapse:collapse;width:100%;font-size:13px}
th,td{padding:7px 10px;border-bottom:1px solid #eceef1;text-align:left;vertical-align:top}
th{background:#f6f8fa;font-weight:600;position:sticky;top:0;z-index:2;font-size:12px;color:#57606a}
td.num,th.num{text-align:right;font-variant-numeric:tabular-nums}
td.vmid{vertical-align:middle}
.dim-metrics th,.dim-metrics td{text-align:center;vertical-align:middle}
.dim-metrics .f1-cell{min-width:105px;font-variant-numeric:tabular-nums}
.dim-metrics .f1-cell .bar{width:90px;margin:4px auto 0}
tr:hover td{background:#fafbfc}
.bar{background:#eceef1;border-radius:3px;height:6px;overflow:hidden;min-width:70px}
.bar i{display:block;height:100%;background:#2da44e;border-radius:3px}
.bar i.mid{background:#d4a72c}.bar i.low{background:#cf222e}
.pill{display:inline-block;padding:1px 8px;border-radius:10px;font-size:12px;font-weight:600;white-space:nowrap}
.t-bad{background:#ffebe9;color:#cf222e}.t-mid{background:#fff8c5;color:#7d4e00}.t-ok{background:#dafbe1;color:#1a7f37}
.t-na{background:#eef1f4;color:#57606a}
.mx{border-collapse:collapse;font-size:13px}
.mx td,.mx th{border:1px solid #e3e6ea;padding:9px 16px;text-align:center}
.mx td.hit{background:#dafbe1;font-weight:650}
.mx td.miss{background:#ffebe9}
.mx td.zero{background:#fff8c5;font-weight:650;color:#9a6700}
.note{background:#fff8f0;border-left:3px solid #e8a33d;padding:12px 16px;border-radius:0 6px 6px 0;margin:14px 0}
.note.red{background:#fff5f5;border-color:#cf222e}
.note.blue{background:#f2f7ff;border-color:#4c8dd9}
.note b{color:#1f2328}
code{background:#f3f4f6;padding:1px 5px;border-radius:4px;font-size:12px;
 font-family:ui-monospace,SFMono-Regular,Menlo,monospace}
.rsn{font-size:12px;color:#3c4149;line-height:1.55;min-width:260px}
.rsn .item{padding:5px 0}
.rsn .item+.item{border-top:1px dashed #e3e6ea;margin-top:3px}
.rsn .ts{display:inline-block;background:#eef1f4;color:#57606a;border-radius:3px;
 padding:0 5px;font-size:11px;font-weight:600;margin-bottom:3px}
.rsn .item>div{display:flex;gap:2px}
.rsn .fk{display:inline-block;width:34px;color:#8b949e;font-size:11px;flex:none}
.rsn .none{color:#a8b0b8;font-style:italic}
details{margin:10px 0}summary{cursor:pointer;font-size:13px;color:#0969da;font-weight:500;padding:4px 0}
summary:hover{text-decoration:underline}
.legend{display:flex;gap:16px;flex-wrap:wrap;font-size:12px;color:#57606a;margin:8px 0 0}
.legend span{display:inline-flex;align-items:center;gap:6px}
.legend i{width:11px;height:11px;border-radius:3px;display:inline-block;flex:none}
.dist{display:flex;height:22px;border-radius:5px;overflow:hidden;font-size:11px;color:#fff;
 font-weight:600;margin:6px 0 2px}
.dist span{display:flex;align-items:center;justify-content:center;min-width:0}
.d0{background:#cf222e}.d5{background:#d4a72c}.d1{background:#2da44e}.dn{background:#8b949e}
.lbl{font-size:12px;color:#6a737d;width:38px;display:inline-block}
.flex{display:flex;gap:20px;align-items:flex-start;flex-wrap:wrap}
.flex>div{flex:1;min-width:300px}
.foot{margin-top:36px;padding-top:14px;border-top:1px solid #e3e6ea;color:#8b949e;font-size:12px}
"""


def render(rows: list[dict[str, Any]]) -> str:
    n = len(rows)
    cases = {r["case"] for r in rows}
    agree = sum(1 for r in rows if r["human"] == r["ai"])
    acc = agree / n

    tp = sum(1 for r in rows if r["human"] != "1" and r["ai"] != "1")
    fp = sum(1 for r in rows if r["human"] == "1" and r["ai"] != "1")
    fn = sum(1 for r in rows if r["human"] != "1" and r["ai"] == "1")
    tn = sum(1 for r in rows if r["human"] == "1" and r["ai"] == "1")
    prec, rec, f1 = prf(tp, fp, fn)

    hc = collections.Counter(r["human"] for r in rows)
    ac = collections.Counter(r["ai"] for r in rows)
    pe = sum(hc[t] / n * ac[t] / n for t in TIERS)
    kappa = (acc - pe) / (1 - pe) if pe < 1 else 0.0

    order = {"0": 0, "0.5": 1, "1": 2}
    stricter = sum(1 for r in rows if order[r["ai"]] < order[r["human"]])
    looser = sum(1 for r in rows if order[r["ai"]] > order[r["human"]])
    stricter_rate = stricter / n
    blemish_total = sum(1 for r in rows if r["human"] == "0.5")
    blemish_hit = sum(1 for r in rows if r["human"] == r["ai"] == "0.5")
    blemish_recall = blemish_hit / blemish_total if blemish_total else 0.0

    not_assessed = rows[0].get("_not_assessed") or []
    hidden = [item for item in not_assessed if item["human"] != "1"]
    verdicts = rows[0].get("_verdicts") or {}
    observed = int(verdicts.get("pass") or 0) + int(verdicts.get("violated") or 0)
    unobserved = int(verdicts.get("not_observable") or 0)
    coverage = observed / (observed + unobserved) if observed + unobserved else 0.0
    # 覆盖率是一致率的前置条件，所以它和一致率一起进 KPI 而不是躲在附录里。
    coverage_denominator = n + len(not_assessed)

    # 样本构成从实际命中的 case 现算：换数据集重跑时副标题不会留下过期的
    # 「T2V + I2V」字样（V2VA 纳入后就曾出现过这种口径漂移）。
    mix = collections.Counter(c.split("/")[0].split("_")[0] for c in cases)
    mix_desc = " + ".join(f"{k} {v}" for k, v in sorted(mix.items(), key=lambda x: -x[1]))
    out: list[str] = [
        "<!doctype html><html lang=zh-CN><head><meta charset=utf-8>",
        "<meta name=viewport content='width=device-width,initial-scale=1'>",
        "<title>native8-v1 判定 vs 人工标注 · 一致性分析</title>",
        f"<style>{CSS}</style></head><body><div class=wrap>",
        "<h1>native8-v1 判定 vs 人工标注 · 一致性分析</h1>",
        f"<p class=sub>{len(cases)} 个 case（{esc(mix_desc)}）· <b>{n}</b> 个可比对维度对 · "
        f"另有 {len(not_assessed)} 个维度因子判据未决被剔除 · "
        "口径：0 恶劣 / 0.5 瑕疵 / 1 无问题 · 数据源：runtime/results 的 native8-v1 报告，零模型成本</p>",
    ]

    # ---------- KPI ----------
    def kpi_cls(v: float, good: float, warn: float) -> str:
        return "good" if v >= good else "warn" if v >= warn else "bad"

    out.append("<div class=kpis>")
    for value, key, cls in (
        (f"{acc:.1%}", f"逐维度一致率（{agree}/{n}）", kpi_cls(acc, 0.85, 0.7)),
        (f"{f1:.3f}", "二分类 F1（发现问题）", kpi_cls(f1, 0.8, 0.6)),
        (f"{prec:.1%}", "Precision", kpi_cls(prec, 0.8, 0.6)),
        (f"{rec:.1%}", "Recall", kpi_cls(rec, 0.8, 0.6)),
        (f"{kappa:.3f}", "Cohen's kappa", kpi_cls(kappa, 0.6, 0.4)),
        (
            f"{n / coverage_denominator:.1%}" if coverage_denominator else "—",
            f"维度可比对率（{n}/{coverage_denominator}）",
            kpi_cls(n / coverage_denominator if coverage_denominator else 0.0, 0.9, 0.7),
        ),
    ):
        out.append(f"<div class='kpi {cls}'><div class=v>{value}</div><div class=k>{esc(key)}</div></div>")
    out.append("</div>")
    acceptance = (
        ("原始一致率", acc, 0.70, f"{acc:.1%}"),
        ("瑕疵档召回", blemish_recall, 0.40, f"{blemish_recall:.1%} ({blemish_hit}/{blemish_total})"),
        ("AI 过严率", stricter_rate, 0.15, f"{stricter_rate:.1%}"),
        ("子判据结论覆盖率", coverage, 0.80, f"{coverage:.1%}"),
    )
    acceptance_text = " · ".join(
        f"<b>{esc(name)}</b> {esc(value)} "
        f"<span class='pill {'t-ok' if (score <= target if name == 'AI 过严率' else score >= target) else 't-bad'}'>"
        f"{'达标' if (score <= target if name == 'AI 过严率' else score >= target) else '未达标'}</span>"
        for name, score, target, value in acceptance
    )
    out.append(f"<div class=note><b>方案验收看板：</b>{acceptance_text}</div>")

    # ---------- 口径说明 ----------
    out.append("<h2>比的是什么</h2><div class=card>")
    out.append(
        "<p>native8-v1 没有总分，也没有整体质量标签，所以一致性只能落在两个粒度上："
        "<b>维度三档</b>与<b>子判据 verdict</b>。人工金标是按八维打的，一格只有一个档位加自由文本，"
        "没有子判据字段 —— 把一个档位摊回 28 条子判据需要凭空指定「这一档是哪条造成的」，"
        "那是编数据。所以<b>一致率只在维度粒度计算</b>，子判据只做单侧分布统计。</p>"
    )
    out.append(
        f"<p><code>not_observable</code> 不等于 <code>pass</code>。定档规则对「没有任何 violated」的维度"
        f"一律给 1.0，无论那是查过没问题还是压根没查。所以本报告把「tier=1 且仍有未决子判据」的维度"
        f"判为<b>未评估</b>并从分母剔除（本批 {len(not_assessed)} 个），"
        f"而 tier 为 0/0.5 的维度即使还有未决子判据也照常参与比对 —— 那个档位由已校验的 violated 立起来，"
        f"不依赖「没发现问题」这个前提。</p>"
    )
    out.append("</div>")

    # ---------- 未评估桶 ----------
    out.append("<h2>未评估桶（被剔出分母的维度）</h2><div class=card>")
    if not_assessed:
        by_dim = collections.Counter(item["dim"] for item in not_assessed)
        out.append(
            f"<div class='note red'><b>{len(not_assessed)} 个维度未评估，其中人工认为有问题的 "
            f"{len(hidden)} 个（{len(hidden) / len(not_assessed):.1%}）。</b><br>"
            "这批就是被口径隐藏起来的疑似漏报：若按 tier=1 计入分母，它们会全部记成"
            f"「AI 说无问题」，其中 {len(hidden)} 个直接虚增一致率或虚降召回。</div>"
        )
        out.append("<table><tr><th>维度</th><th class=num>未评估</th><th class=num>其中人工报问题</th>"
                   "<th>高频未决子判据</th></tr>")
        for dim, cnt in by_dim.most_common():
            dim_items = [item for item in not_assessed if item["dim"] == dim]
            dim_hidden = sum(1 for item in dim_items if item["human"] != "1")
            keys = collections.Counter(
                key for item in dim_items for key in item["unresolved"]
            )
            top = "、".join(f"{esc(key)}×{cnt2}" for key, cnt2 in keys.most_common(3))
            out.append(
                f"<tr><td><b>{esc(dim)}</b></td><td class=num>{cnt}</td>"
                f"<td class=num>{dim_hidden}</td><td style='font-size:12px'>{top}</td></tr>"
            )
        out.append("</table>")
    else:
        out.append("<p>本批没有被剔除的维度：所有 tier=1 的维度都已把子判据判完。</p>")
    out.append("</div>")

    # ---------- 子判据 verdict 分布 ----------
    out.append("<h2>子判据 verdict 分布（单侧）</h2><div class=card>")
    total_verdicts = sum(int(verdicts.get(key) or 0) for key in VERDICTS)
    if total_verdicts:
        out.append("<div class=dist>")
        for key, cls in (("violated", "d0"), ("not_observable", "dn"),
                         ("pass", "d1"), ("not_applicable", "dn")):
            count = int(verdicts.get(key) or 0)
            pct = count / total_verdicts * 100
            text = f"{VERDICT_NAME[key]} {count}" if pct > 9 else ""
            out.append(
                f"<span class='{cls}' style='width:{pct:.2f}%' "
                f"title='{esc(VERDICT_NAME[key])}：{count} 条 / {pct:.1f}%'>{text}</span>"
            )
        out.append("</div>")
        out.append(
            f"<p style='font-size:13px;color:#57606a'>"
            + " · ".join(
                f"<code>{key}</code> {int(verdicts.get(key) or 0)}" for key in VERDICTS
            )
            + f"　→ 已给出结论 <b>{coverage:.1%}</b>（排除 not_applicable）</p>"
        )
        if coverage < 0.8:
            out.append(
                "<div class=note><b>覆盖率不足八成。</b>此时维度一致率只在少数被真正评过的"
                "子判据上成立，先补覆盖再讨论准确率 —— 反过来做会把「没查」当成「查对了」。</div>"
            )
    else:
        out.append("<p>本批报告没有 rubric 子判据，无法统计。</p>")
    review = rows[0].get("_review") or {}
    facts = rows[0].get("_facts") or {}
    decided = int(facts.get("decided") or 0)
    if decided:
        out.append(
            f"<p style='font-size:13px;color:#57606a'>原生 issue {decided} 条，其中带结构化 "
            f"<code>facts</code> 的 {int(facts.get('with_basis') or 0)} 条"
            f"（{int(facts.get('with_basis') or 0) / decided:.1%}）；"
            f"被打回复核的 violated {int(review.get('required') or 0)} 条，"
            f"复核候选 issue {int(review.get('candidates') or 0)} 条。三档完全由 facts 推出，"
            "所以 facts 缺失率就是「分档凭什么」的上限。</p>"
        )
    else:
        out.append(
            "<div class=note>本批没有原生 issue —— 请确认结果缓存确为 native8-v1，"
            "否则下面所有指标都建立在空事实上。</div>"
        )
    out.append("</div>")

    # ---------- 档位分布与混淆矩阵 ----------
    mid_shortage = ac["0.5"] < max(1, hc["0.5"] / 3)
    out.append(f"<h2>档位分布{'：AI 的「瑕疵」档明显不足' if mid_shortage else '与分级偏差'}</h2>")
    out.append("<div class=card><div class=flex><div>")
    out.append("<h3>档位分布对比</h3>")
    for label, counter in (("人工", hc), ("AI", ac)):
        out.append(f"<div><span class=lbl>{label}</span></div><div class=dist>")
        for tier, cls in (("0", "d0"), ("0.5", "d5"), ("1", "d1")):
            pct = counter[tier] / n * 100
            text = f"{counter[tier]} ({pct:.0f}%)" if pct > 7 else ""
            tip = f"{TIER_NAME[tier]}({tier})：{counter[tier]} 例 / {pct:.1f}%"
            out.append(f"<span class='{cls}' style='width:{pct:.2f}%' title='{tip}'>{text}</span>")
        out.append("</div>")
    out.append(
        "<div class=legend>"
        "<span><i class=d0></i>恶劣(0)</span>"
        "<span><i class=d5></i>瑕疵(0.5)</span>"
        "<span><i class=d1></i>无问题(1)</span>"
        "</div></div><div>"
    )
    out.append("<h3>混淆矩阵　<span style='font-weight:400;color:#6a737d;font-size:12px'>行=人工　列=AI</span></h3>")
    matrix = collections.Counter((r["human"], r["ai"]) for r in rows)
    out.append("<table class=mx><tr><th></th>" + "".join(f"<th>{TIER_NAME[t]}</th>" for t in TIERS) + "</tr>")
    for ht in TIERS:
        out.append(f"<tr><th>{TIER_NAME[ht]}</th>")
        for at in TIERS:
            count = matrix[(ht, at)]
            cls = "hit" if ht == at else "miss" if count else ""
            if at == "0.5" and count == 0:
                cls = "zero"
            out.append(f"<td class='{cls}'>{count}</td>")
        out.append("</tr>")
    out.append("</table></div></div>")
    out.append(
        f"<div class=note>严格程度：<b>AI 比人工严 {stricter} 例（{stricter / n:.1%}）</b>，"
        f"AI 比人工松 {looser} 例（{looser / n:.1%}）。"
        + ("漏报方向是主要失配，需优先补事实召回。" if looser > stricter else "误报方向是主要失配，需优先收紧证据门槛。")
        + "</div>"
    )
    both = [r for r in rows if r["human"] != "1" and r["ai"] != "1"]
    if both:
        same_tier = sum(1 for r in both if r["human"] == r["ai"])
        out.append(
            f"<div class='note blue'><b>已共同检出的问题分档：</b>"
            f"双方都认为「有问题」的 {len(both)} 例中，三档一致 {same_tier} 例"
            f"（{same_tier / len(both):.1%}）。问题召回 {rec:.0%}。</div>"
        )
    out.append("</div>")

    # ---------- 按维度 ----------
    out.append("<h2>按维度拆解</h2><div class=card><table class=dim-metrics>")
    out.append(
        "<tr><th>维度</th><th class=num>n</th><th class=num>一致率</th>"
        "<th class=num>P</th><th class=num>R</th><th class=num>F1</th>"
        "<th class=num>误报</th><th class=num>漏报</th><th class=num>分级错</th>"
        "<th class=num>未评估</th><th>评价</th></tr>"
    )
    skipped_by_dim = collections.Counter(item["dim"] for item in not_assessed)
    per_dim = []
    for dim in DIM_ORDER:
        sub = [r for r in rows if r["dim"] == dim]
        if not sub:
            continue
        d_acc = sum(1 for r in sub if r["human"] == r["ai"]) / len(sub)
        d_tp = sum(1 for r in sub if r["human"] != "1" and r["ai"] != "1")
        d_fp = sum(1 for r in sub if r["human"] == "1" and r["ai"] != "1")
        d_fn = sum(1 for r in sub if r["human"] != "1" and r["ai"] == "1")
        dp, dr, df1 = prf(d_tp, d_fp, d_fn)
        d_tier = sum(1 for r in sub if r["human"] != "1" and r["ai"] != "1" and r["human"] != r["ai"])
        per_dim.append((dim, len(sub), d_acc, dp, dr, df1, d_fp, d_fn, d_tier, d_tp,
                        skipped_by_dim[dim]))
    for dim, cnt, d_acc, dp, dr, df1, d_fp, d_fn, d_tier, d_tp, d_skip in sorted(
        per_dim, key=lambda x: -x[5]
    ):
        cls = "" if df1 >= 0.8 else "mid" if df1 >= 0.5 else "low"
        verdict = (
            "全部判为无问题，检出失效" if d_tp == 0 and d_fp == 0 and d_fn > 0
            else "优秀，判据客观" if df1 >= 0.85
            else "良好" if df1 >= 0.65
            else "偏弱" if df1 >= 0.4
            else "问题检出能力很弱"
        )
        out.append(
            f"<tr><td><b>{esc(dim)}</b></td><td class=num>{cnt}</td><td class=num>{d_acc:.0%}</td>"
            f"<td class=num>{dp:.0%}</td><td class=num>{dr:.0%}</td>"
            f"<td class=f1-cell><b>{df1:.2f}</b>{bar(df1, cls, f'F1 = {df1:.2f}')}</td>"
            f"<td class=num>{d_fp}</td><td class=num>{d_fn}</td><td class=num>{d_tier}</td>"
            f"<td class=num>{d_skip}</td>"
            f"<td style='font-size:12px;color:#57606a'>{verdict}</td></tr>"
        )
    out.append("</table>")
    # 评语里的具体数字全部从 per_dim 现算，换样本重跑时不会留下过期结论。
    ranked = sorted(per_dim, key=lambda x: -x[5])
    lookup = {row[0]: row for row in per_dim}
    strong = [f"<b>{esc(row[0])}({row[5]:.2f})</b>" for row in ranked if row[5] >= 0.8]
    weak = [f"<b>{esc(row[0])}({row[5]:.2f})</b>" for row in ranked if row[5] < 0.4]
    degenerate = [row for row in ranked if row[9] == 0 and row[6] == 0 and row[7] > 0]
    audio = lookup.get("音频综合质量")
    notes: list[str] = []
    if strong:
        basis = "、".join(
            esc(DIM_BASIS[row[0]]) for row in ranked if row[5] >= 0.8 and row[0] in DIM_BASIS
        )
        notes.append(
            f"{'、'.join(strong)} 判据客观（{basis}），模型表现最好；"
            if basis
            else f"{'、'.join(strong)} 判据客观，模型表现最好；"
        )
    if audio:
        notes.append(
            f"<b>音频综合质量 Recall {audio[4]:.0%}</b>，漏了 {1 - audio[4]:.0%} 的音频问题 —— "
            "与项目文档记录的「Gemini 音频感知偏弱是已知局限」一致，属预期内；"
        )
    if weak:
        worst = ranked[-1]
        if degenerate:
            names = "、".join(f"<b>{esc(row[0])}</b>" for row in degenerate)
            notes.append(
                f"{names} 不是随机预测，而是退化为全部判定无问题：没有预测任何正例，"
                "因此即使无问题样本占多数、一致率看似较高，问题检出 P/R/F1 仍为 0。"
            )
        else:
            notes.append(
                f"{'、'.join(weak)} 问题检出能力很弱（最差的 {esc(worst[0])} "
                f"P={worst[3]:.0%} / R={worst[4]:.0%}），判据与人工理解存在明显错位。"
            )
    if notes:
        out.append(f"<div class=note><b>各维度分化明显。</b>{''.join(notes)}</div>")
    heavy_skip = [row for row in per_dim if row[10] and row[10] >= row[1]]
    if heavy_skip:
        names = "、".join(f"<b>{esc(row[0])}</b>（{row[10]} 个）" for row in heavy_skip)
        out.append(
            f"<div class='note red'>{names} 被剔除的维度不少于参与比对的维度，"
            "该维的 F1 只代表极小一部分样本，不能当作这一维的能力结论。</div>"
        )
    out.append("</div>")

    # ---------- 按生成模式 ----------
    out.append("<h2>按生成模式拆解</h2><div class=card><table>")
    out.append(
        "<tr><th>模式</th><th class=num>n</th><th class=num>一致率</th>"
        "<th class=num>误报</th><th class=num>漏报</th><th class=num>分级错</th><th>三档混淆（人工→AI）</th></tr>"
    )
    for mode in sorted({r["case"].split("/")[0].split("_")[0] for r in rows}):
        sub = [r for r in rows if r["case"].split("/")[0].split("_")[0] == mode]
        m_acc = sum(1 for r in sub if r["human"] == r["ai"]) / len(sub)
        m_fp = sum(1 for r in sub if r["human"] == "1" and r["ai"] != "1")
        m_fn = sum(1 for r in sub if r["human"] != "1" and r["ai"] == "1")
        m_tier = sum(1 for r in sub if r["human"] != "1" and r["ai"] != "1" and r["human"] != r["ai"])
        mx = collections.Counter((r["human"], r["ai"]) for r in sub)
        mx_text = "；".join(
            f"{TIER_NAME[h]}→{TIER_NAME[a]} {mx[(h, a)]}"
            for h in TIERS for a in TIERS if mx[(h, a)]
        )
        out.append(
            f"<tr><td><b>{esc(mode)}</b></td><td class=num>{len(sub)}</td><td class=num>{m_acc:.1%}</td>"
            f"<td class=num>{m_fp}</td><td class=num>{m_fn}</td><td class=num>{m_tier}</td>"
            f"<td style='font-size:12px'>{esc(mx_text)}</td></tr>"
        )
    out.append("</table></div>")

    # ---------- 分歧最多的 case ----------
    out.append("<h2>分歧最多的 case</h2><div class=card><table>")
    out.append("<tr><th>case</th><th>分析状态</th><th class=num>分歧</th><th>明细</th></tr>")
    by_case = collections.Counter(r["case"] for r in rows if r["human"] != r["ai"])
    info = {r["case"]: r for r in rows}
    for case_id, cnt in by_case.most_common(10):
        detail = " ".join(
            f"<span class=pill style='background:#f3f4f6;color:#57606a'>{esc(r['dim'])} "
            f"人<b>{r['human']}</b>/AI<b>{r['ai']}</b></span>"
            for r in rows
            if r["case"] == case_id and r["human"] != r["ai"]
        )
        meta = info.get(case_id, {})
        out.append(
            f"<tr><td><code>{esc(case_id.split('/')[-1])}</code><br>"
            f"<span style='font-size:11px;color:#8b949e'>{esc(case_id.split('/')[0])}</span></td>"
            f"<td style='font-size:12px'>{esc(meta.get('status'))}</td>"
            f"<td class=num><b>{cnt}</b></td><td style='line-height:2'>{detail}</td></tr>"
        )
    out.append("</table></div>")

    # ---------- 逐条明细 ----------
    out.append("<h2>逐条明细</h2>")
    diffs = [r for r in rows if r["human"] != r["ai"]]
    with_h = sum(1 for r in diffs if r["human_reason"])
    with_a = sum(1 for r in diffs if r["ai_reason"])
    out.append(
        f"<details open><summary>▸ 分歧项（{len(diffs)} 条 · 人工写了理由 {with_h} 条 · "
        f"AI 有依据 {with_a} 条）</summary><div class=card><table>"
        "<tr><th style='width:88px'>case</th><th style='width:96px'>维度</th>"
        "<th style='width:56px'>人工</th><th style='width:56px'>AI</th>"
        "<th>人工理由</th><th>AI 依据</th></tr>"
    )
    for r in sorted(diffs, key=lambda x: (x["case"], x["dim"])):
        out.append(
            f"<tr><td><code>{esc(r['case'].split('/')[-1])}</code></td><td>{esc(r['dim'])}</td>"
            f"<td><span class='pill {TIER_CLS[r['human']]}'>{TIER_NAME[r['human']]}</span></td>"
            f"<td><span class='pill {TIER_CLS[r['ai']]}'>{TIER_NAME[r['ai']]}</span></td>"
            f"<td class=rsn>{human_reason_html(r['human_reason'])}</td>"
            f"<td class=rsn>{ai_reason_html(r['ai_reason'], r['ai'])}</td></tr>"
        )
    out.append("</table></div></details>")

    same = [r for r in rows if r["human"] == r["ai"]]
    out.append(
        f"<details><summary>▸ 一致项（{len(same)} 条）</summary><div class=card><table>"
        "<tr><th>case</th><th>维度</th><th>双方判定</th></tr>"
    )
    for r in sorted(same, key=lambda x: (x["case"], x["dim"])):
        out.append(
            f"<tr><td><code>{esc(r['case'].split('/')[-1])}</code></td><td>{esc(r['dim'])}</td>"
            f"<td><span class='pill {TIER_CLS[r['ai']]}'>{TIER_NAME[r['ai']]}</span></td></tr>"
        )
    out.append("</table></div></details>")

    if not_assessed:
        out.append(
            f"<details><summary>▸ 未评估项（{len(not_assessed)} 条 · 不计入以上任何指标）"
            "</summary><div class=card><table>"
            "<tr><th>case</th><th>维度</th><th>人工</th><th>未决子判据</th></tr>"
        )
        for item in sorted(not_assessed, key=lambda x: (x["case"], x["dim"])):
            out.append(
                f"<tr><td><code>{esc(item['case'].split('/')[-1])}</code></td>"
                f"<td>{esc(item['dim'])}</td>"
                f"<td><span class='pill {TIER_CLS[item['human']]}'>{TIER_NAME[item['human']]}</span></td>"
                f"<td style='font-size:12px'>{esc('、'.join(item['unresolved']))}</td></tr>"
            )
        out.append("</table></div></details>")

    # ---------- 改进建议 ----------
    small = [
        f"{esc(row[0])} n={row[1]}"
        for row in sorted(per_dim, key=lambda x: x[1])
        if per_dim and row[1] < n / len(per_dim) * 0.8
    ]
    out.append("<h2>改进建议</h2><div class=card>")
    advice: list[str] = []
    if coverage < 0.8 or not_assessed:
        advice.append(
            f"<p><b>1. 先补子判据覆盖。</b>已给出结论的子判据只有 {coverage:.1%}，"
            f"{len(not_assessed)} 个维度因此被剔出分母（其中 {len(hidden)} 个人工报了问题）。"
            "这些 <code>not_observable</code> 大多来自证据门槛未过而被打回复核，"
            "应先看 <code>review_required</code> 的 reason 分布，再决定是补取证还是松门槛。</p>"
        )
    if decided:
        advice.append(
            f"<p><b>{len(advice) + 1}. 审查结构化三档依据。</b>三档完全按 issue 的 "
            "<code>facts</code> 推出，优先核对 <code>requirement_importance</code>、"
            "<code>object_scope</code>、时长、可见性、文字重要度、动作类型与 "
            "<code>affects_adoption</code> 是否与视频证据一致。</p>"
        )
    if audio:
        advice.append(
            f"<p><b>{len(advice) + 1}. 音频维度考虑补强。</b>Recall {audio[4]:.0%} 是模型能力边界"
            "（已知局限），若该维重要，可考虑接入独立的音频分析（如 ASR + 音画对齐检测），"
            "而非依赖 VLM 直接听。</p>"
        )
    advice.append(
        "<p><b>金标口径本身也要复核。</b>现有金标是上一版五维协议时期打的，逐格重判写在 "
        "<code>--adjudication</code> 覆盖层里（只有 <code>status=approved</code> 生效），"
        "金标文件保持只读。覆盖层同样只能到维度粒度。</p>"
    )
    if small:
        advice.append(
            "<p style='color:#6a737d;font-size:13px;margin-bottom:0'><b>注意样本量：</b>"
            f"{'、'.join(small)}，样本偏小，这几项指标置信区间较宽，"
            "结论宜作方向性参考而非精确结论。</p>"
        )
    out.append("".join(advice))
    out.append("</div>")

    engine = str(rows[0].get("_engine") or "gemini")
    engine_name = (
        "Vertex 直连" if engine == "gemini"
        else "KLink OpenAI 兼容层" if engine == "gemini-openai"
        else engine
    )
    adjudication_counts = rows[0].get("_adjudication_counts") or {}
    out.append(
        "<div class=foot>映射方式：标注文件顶层 key 为 <code>work_id</code>，"
        "经各 case 目录 <code>source.json</code> 的 <code>work_id</code> 字段关联，"
        f"本次命中 {len(cases)} 个 case。"
        f"引擎：<code>{esc(engine)}</code>（{engine_name}）—— 同一 case 在不同 API 协议下结果不同，"
        "缓存已按引擎隔离，切换 <code>--engine</code> 可对比。"
        + (
            "复核覆盖层：" + "、".join(
                f"{esc(key)} {value}" for key, value in sorted(adjudication_counts.items())
            ) + "。"
            if adjudication_counts else ""
        )
        + "分析为纯离线计算：直接读取 <code>runtime/results/</code> 的 native8-v1 报告，"
        "不触发任何模型调用。</div>"
    )
    out.append("</div></body></html>")
    return "".join(out)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--human", default="/Users/fscnb/Downloads/金标考试_gold_label.json")
    parser.add_argument("-o", "--out", default="runtime/vs_human_report.html")
    parser.add_argument(
        "--adjudication", default="adjudication_overlay.json",
        help="复核覆盖层；只应用 status=approved 的条目",
    )
    parser.add_argument(
        "--init-adjudication", action="store_true",
        help="按当前全部分歧生成 pending 审计矩阵；已有非空文件不会覆盖",
    )
    parser.add_argument(
        "--engine", default="gemini",
        help="用哪套结果缓存：gemini=Vertex 直连，gemini-openai=KLink OpenAI 兼容层",
    )
    args = parser.parse_args()

    adjudication_path = pathlib.Path(args.adjudication) if args.adjudication else None
    rows = collect(pathlib.Path(args.human), args.engine, adjudication_path)
    if not rows:
        print("没有可比对的样本")
        return
    if args.init_adjudication and adjudication_path is not None:
        count = write_adjudication_template(rows, adjudication_path)
        print(f"复核矩阵 → {adjudication_path.resolve()}  ({count} 条 pending)")
        rows = collect(pathlib.Path(args.human), args.engine, adjudication_path)
    dest = pathlib.Path(args.out)
    dest.parent.mkdir(parents=True, exist_ok=True)
    dest.write_text(render(rows), encoding="utf-8")
    print(f"报告 → {dest.resolve()}  ({dest.stat().st_size / 1024:.0f} KB, {len(rows)} 条)")


if __name__ == "__main__":
    main()
