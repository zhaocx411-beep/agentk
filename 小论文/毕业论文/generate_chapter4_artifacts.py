from __future__ import annotations

import csv
import json
import math
import random
from collections import defaultdict
from pathlib import Path

BASE_DIR = Path("/Users/apple/Desktop/毕业设计/小论文")
FORMAL_DIR = BASE_DIR / "SEAC_INDUSTRY/experiments/remote_formal_20260824/experiments/formal_v1"
WORKBENCH_DIR = BASE_DIR / "SEAC_INDUSTRY/workbench"
THESIS_DIR = BASE_DIR / "毕业论文"
RESULTS_DIR = THESIS_DIR / "chapter4_results"
FIG_DIR = THESIS_DIR / "figures"

RESULTS_DIR.mkdir(parents=True, exist_ok=True)
FIG_DIR.mkdir(parents=True, exist_ok=True)

DEV_VAL_PROJECTS = ["P01", "P02", "P03"]
LOCKED_TEST_PROJECTS = ["P04", "P05", "P06", "P07", "P08", "P09"]
ALL_PROJECTS = DEV_VAL_PROJECTS + LOCKED_TEST_PROJECTS

ROLE_MAPPING = {
    "Database": {
        "agent_role": "database_agent",
        "agent_role_cn": "数据库设计智能体",
        "agent_layer": "执行层 (模块级)",
        "demand_type": "atomic_fact",
        "demand_type_cn": "精确事实型 (表行/字段)",
        "routed_alpha_2tier": 0.0,
        "routed_alpha_3tier": 0.0,
        "return_mode": "child_only",
        "return_mode_cn": "仅子块模式 (带表头与出处)",
    },
    "Constraint": {
        "agent_role": "constraint_agent",
        "agent_role_cn": "规格约束核查智能体",
        "agent_layer": "执行层 (模块级)",
        "demand_type": "atomic_fact",
        "demand_type_cn": "精确事实型 (单一条款)",
        "routed_alpha_2tier": 0.0,
        "routed_alpha_3tier": 0.0,
        "return_mode": "child_only",
        "return_mode_cn": "仅子块模式 (带表头与出处)",
    },
    "Numeric constraint": {
        "agent_role": "constraint_agent",
        "agent_role_cn": "规格约束核查智能体",
        "agent_layer": "执行层 (模块级)",
        "demand_type": "atomic_fact",
        "demand_type_cn": "精确事实型 (数值指标)",
        "routed_alpha_2tier": 0.0,
        "routed_alpha_3tier": 0.0,
        "return_mode": "child_only",
        "return_mode_cn": "仅子块模式 (带表头与出处)",
    },
    "Meeting fact": {
        "agent_role": "requirement_trace_agent",
        "agent_role_cn": "需求溯源核查智能体",
        "agent_layer": "执行层 (模块级)",
        "demand_type": "atomic_fact",
        "demand_type_cn": "精确事实型 (纪要表行)",
        "routed_alpha_2tier": 0.0,
        "routed_alpha_3tier": 0.0,
        "return_mode": "child_only",
        "return_mode_cn": "仅子块模式 (带表头与出处)",
    },
    "API": {
        "agent_role": "interface_agent",
        "agent_role_cn": "接口设计智能体",
        "agent_layer": "模块层 (跨块关联)",
        "demand_type": "hierarchical_context",
        "demand_type_cn": "层次上下文型 (接口定义)",
        "routed_alpha_2tier": 0.35,
        "routed_alpha_3tier": 0.35,
        "return_mode": "hybrid",
        "return_mode_cn": "父子混合模式 (父块+触发子块)",
    },
    "API parameter": {
        "agent_role": "interface_agent",
        "agent_role_cn": "接口设计智能体",
        "agent_layer": "模块层 (跨块关联)",
        "demand_type": "hierarchical_context",
        "demand_type_cn": "层次上下文型 (接口参数)",
        "routed_alpha_2tier": 0.35,
        "routed_alpha_3tier": 0.35,
        "return_mode": "hybrid",
        "return_mode_cn": "父子混合模式 (父块+触发子块)",
    },
    "Feature": {
        "agent_role": "overview_agent",
        "agent_role_cn": "章节概述智能体",
        "agent_layer": "协调/章节层",
        "demand_type": "macro_overview",
        "demand_type_cn": "上下文概述型 (功能架构)",
        "routed_alpha_2tier": 0.35,
        "routed_alpha_3tier": 0.0,
        "return_mode": "parent_only",
        "return_mode_cn": "仅父块模式 (完整段落上下文)",
    },
    "Process": {
        "agent_role": "detailed_design_agent",
        "agent_role_cn": "总体编排与流程智能体",
        "agent_layer": "协调/章节层",
        "demand_type": "macro_overview",
        "demand_type_cn": "上下文概述型 (业务流程)",
        "routed_alpha_2tier": 0.0,
        "routed_alpha_3tier": 1.0,
        "return_mode": "parent_only",
        "return_mode_cn": "仅父块模式 (完整段落上下文)",
    },
}


def load_project_token_stats():
    stats = {}
    for p in ALL_PROJECTS:
        res = json.loads((FORMAL_DIR / p / "results.json").read_text(encoding="utf-8"))
        p_cnt = res["proposed_v6"]["parent_chunk_count"]
        c_cnt = res["proposed_v6"]["child_chunk_count"]
        tot_cnt = res["proposed_v6"]["chunk_count"]
        p_mean = res["proposed"]["mean_chunk_tokens"]
        tot_mean = res["proposed_v6"]["mean_chunk_tokens"]
        c_mean = (tot_mean * tot_cnt - p_mean * p_cnt) / c_cnt
        stats[p] = {
            "parent_count": p_cnt,
            "child_count": c_cnt,
            "parent_mean_tokens": p_mean,
            "child_mean_tokens": c_mean,
            "hybrid_mean_tokens": p_mean + c_mean,
            "fixed_mean_tokens": res["fixed"]["mean_chunk_tokens"],
        }
    return stats


def load_all_questions(proj_tokens):
    rows = []
    for p in ALL_PROJECTS:
        split = "dev_val" if p in DEV_VAL_PROJECTS else "locked_test"
        qa_list = {x["qa_id"]: x for x in json.loads((WORKBENCH_DIR / p / "qa_approved.json").read_text(encoding="utf-8"))}
        d_nf = {x["qa_id"]: x for x in json.loads((FORMAL_DIR / p / "seac_no_fusion_retrieval_details.json").read_text(encoding="utf-8"))}
        d_v6 = {x["qa_id"]: x for x in json.loads((FORMAL_DIR / p / "proposed_v6_retrieval_details.json").read_text(encoding="utf-8"))}
        d_pr = {x["qa_id"]: x for x in json.loads((FORMAL_DIR / p / "proposed_retrieval_details.json").read_text(encoding="utf-8"))}
        d_fx = {x["qa_id"]: x for x in json.loads((FORMAL_DIR / p / "fixed_retrieval_details.json").read_text(encoding="utf-8"))}
        d_st = {x["qa_id"]: x for x in json.loads((FORMAL_DIR / p / "structure_retrieval_details.json").read_text(encoding="utf-8"))}

        for qid, v6_item in d_v6.items():
            q_meta = qa_list.get(qid, {})
            qtype = v6_item["question_type"]
            rmap = ROLE_MAPPING[qtype]
            rows.append({
                "project_id": p,
                "split": split,
                "qa_id": qid,
                "question": q_meta.get("question", ""),
                "source_document": v6_item["source_document"],
                "question_type": qtype,
                "agent_role": rmap["agent_role"],
                "agent_role_cn": rmap["agent_role_cn"],
                "agent_layer": rmap["agent_layer"],
                "demand_type": rmap["demand_type"],
                "demand_type_cn": rmap["demand_type_cn"],
                "return_mode": rmap["return_mode"],
                "return_mode_cn": rmap["return_mode_cn"],
                "rank_alpha_0_0": d_nf[qid]["rank"],
                "rank_alpha_0_35": d_v6[qid]["rank"],
                "rank_alpha_1_0": d_pr[qid]["rank"],
                "rank_fixed": d_fx[qid]["rank"],
                "rank_structure": d_st[qid]["rank"],
                "span_rank_alpha_0_0": d_nf[qid]["span_rank"],
                "span_rank_alpha_0_35": d_v6[qid]["span_rank"],
                "span_rank_alpha_1_0": d_pr[qid]["span_rank"],
                "parent_tokens_top1": proj_tokens[p]["parent_mean_tokens"],
                "child_tokens_top1": proj_tokens[p]["child_mean_tokens"],
                "hybrid_tokens_top1": proj_tokens[p]["hybrid_mean_tokens"],
                "fixed_tokens_top1": proj_tokens[p]["fixed_mean_tokens"],
            })
    return rows


def metrics_from_ranks(ranks, span_ranks, top1_tokens):
    n = len(ranks)
    rr = [(1.0 / r if r else 0.0) for r in ranks]
    s_rr = [(1.0 / r if r else 0.0) for r in span_ranks]
    h1 = [1.0 if (r and r <= 1) else 0.0 for r in ranks]
    h3 = [1.0 if (r and r <= 3) else 0.0 for r in ranks]
    h5 = [1.0 if (r and r <= 5) else 0.0 for r in ranks]
    return {
        "count": n,
        "mrr": sum(rr) / n,
        "hit@1": sum(h1) / n,
        "hit@3": sum(h3) / n,
        "hit@5": sum(h5) / n,
        "span_mrr": sum(s_rr) / n,
        "avg_top1_tokens": sum(top1_tokens) / n,
        "avg_top5_tokens": sum(t * 5 for t in top1_tokens) / n,
    }


def hierarchical_bootstrap_ci(rows, score_fn, n_boot=10000, seed=20260804):
    rng = random.Random(seed)
    by_proj = defaultdict(list)
    for r in rows:
        by_proj[r["project_id"]].append(r)
    projs = sorted(by_proj.keys())
    k = len(projs)
    samples = []
    for _ in range(n_boot):
        sampled_projs = [rng.choice(projs) for _ in range(k)]
        vals = []
        for p in sampled_projs:
            items = by_proj[p]
            m = len(items)
            for _ in range(m):
                vals.append(score_fn(rng.choice(items)))
        samples.append(sum(vals) / len(vals))
    samples.sort()
    return [samples[int(0.025 * n_boot)], samples[int(0.975 * n_boot)]]


def exact_cluster_paired_test(rows, score_fn_a, score_fn_b):
    by_proj = defaultdict(list)
    for r in rows:
        by_proj[r["project_id"]].append(score_fn_a(r) - score_fn_b(r))
    projs = sorted(by_proj.keys())
    k = len(projs)
    total_n = len(rows)
    obs_diff = sum(sum(by_proj[p]) for p in projs) / total_n
    proj_sums = [sum(by_proj[p]) for p in projs]
    extreme = 0
    total_perms = 1 << k
    for mask in range(total_perms):
        perm_sum = 0.0
        for i in range(k):
            sign = 1.0 if (mask & (1 << i)) else -1.0
            perm_sum += sign * proj_sums[i]
        if abs(perm_sum / total_n) >= abs(obs_diff) - 1e-12:
            extreme += 1
    return {
        "mean_difference": obs_diff,
        "p_value_exact": extreme / total_perms,
        "positive_projects": sum(1 for s in proj_sums if s > 1e-9),
        "negative_projects": sum(1 for s in proj_sums if s < -1e-9),
        "tied_projects": sum(1 for s in proj_sums if abs(s) <= 1e-9),
    }


def evaluate_configurations(rows):
    configs = {}

    def eval_cfg(name, rank_fn, span_fn, tok_fn):
        ranks = [rank_fn(r) for r in rows]
        sranks = [span_fn(r) for r in rows]
        toks = [tok_fn(r) for r in rows]
        m = metrics_from_ranks(ranks, sranks, toks)
        m["mrr_ci95"] = hierarchical_bootstrap_ci(rows, lambda r: 1.0 / rank_fn(r) if rank_fn(r) else 0.0)
        configs[name] = m

    eval_cfg("fixed_window", lambda r: r["rank_fixed"], lambda r: r["rank_fixed"], lambda r: r["fixed_tokens_top1"])
    eval_cfg("seac_parent_only_alpha_1.0", lambda r: r["rank_alpha_1_0"], lambda r: r["span_rank_alpha_1_0"], lambda r: r["parent_tokens_top1"])
    eval_cfg("seac_default_alpha_0.35", lambda r: r["rank_alpha_0_35"], lambda r: r["span_rank_alpha_0_35"], lambda r: r["parent_tokens_top1"])
    eval_cfg("seac_no_fusion_alpha_0.0_parent_return", lambda r: r["rank_alpha_0_0"], lambda r: r["span_rank_alpha_0_0"], lambda r: r["parent_tokens_top1"])
    eval_cfg("seac_no_fusion_alpha_0.0_child_return", lambda r: r["rank_alpha_0_0"], lambda r: r["span_rank_alpha_0_0"], lambda r: r["child_tokens_top1"])

    def tok_dyn(r):
        if r["demand_type"] == "atomic_fact":
            return r["child_tokens_top1"]
        elif r["demand_type"] == "hierarchical_context":
            return r["hybrid_tokens_top1"]
        else:
            return r["parent_tokens_top1"]

    def rank_dyn_recall(r):
        if r["question_type"] in {"API", "API parameter", "Feature"}:
            return r["rank_alpha_0_35"]
        return r["rank_alpha_0_0"]

    def srank_dyn_recall(r):
        if r["question_type"] in {"API", "API parameter", "Feature"}:
            return r["span_rank_alpha_0_35"]
        return r["span_rank_alpha_0_0"]

    eval_cfg("mas_dynamic_recall_oriented", rank_dyn_recall, srank_dyn_recall, tok_dyn)

    def rank_dyn_prec(r):
        if r["question_type"] in {"API", "API parameter"}:
            return r["rank_alpha_0_35"]
        return r["rank_alpha_0_0"]

    def srank_dyn_prec(r):
        if r["question_type"] in {"API", "API parameter"}:
            return r["span_rank_alpha_0_35"]
        return r["span_rank_alpha_0_0"]

    eval_cfg("mas_dynamic_precision_oriented", rank_dyn_prec, srank_dyn_prec, tok_dyn)

    def rank_dyn_3tier(r):
        if r["question_type"] in {"API", "API parameter"}:
            return r["rank_alpha_0_35"]
        elif r["question_type"] == "Process":
            return r["rank_alpha_1_0"]
        return r["rank_alpha_0_0"]

    def srank_dyn_3tier(r):
        if r["question_type"] in {"API", "API parameter"}:
            return r["span_rank_alpha_0_35"]
        elif r["question_type"] == "Process":
            return r["span_rank_alpha_1_0"]
        return r["span_rank_alpha_0_0"]

    eval_cfg("mas_dynamic_3tier", rank_dyn_3tier, srank_dyn_3tier, tok_dyn)

    # Paired tests for mas_dynamic_precision_oriented vs default (0.35) and vs global best (0.0)
    configs["paired_tests_precision_routed"] = {
        "vs_seac_default_0.35": exact_cluster_paired_test(
            rows,
            lambda r: 1.0 / rank_dyn_prec(r) if rank_dyn_prec(r) else 0.0,
            lambda r: 1.0 / r["rank_alpha_0_35"] if r["rank_alpha_0_35"] else 0.0,
        ),
        "vs_seac_no_fusion_0.0": exact_cluster_paired_test(
            rows,
            lambda r: 1.0 / rank_dyn_prec(r) if rank_dyn_prec(r) else 0.0,
            lambda r: 1.0 / r["rank_alpha_0_0"] if r["rank_alpha_0_0"] else 0.0,
        ),
        "vs_seac_parent_only_1.0": exact_cluster_paired_test(
            rows,
            lambda r: 1.0 / rank_dyn_prec(r) if rank_dyn_prec(r) else 0.0,
            lambda r: 1.0 / r["rank_alpha_1_0"] if r["rank_alpha_1_0"] else 0.0,
        ),
    }
    return configs


def _svg_escape(s: str) -> str:
    return s.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;").replace('"', "&quot;")


def generate_svg_figures(all_rows, summary):
    locked_rows = [r for r in all_rows if r["split"] == "locked_test"]

    # 1. fig4_1_question_type_mrr.svg
    qtypes = ["Database", "Constraint", "Meeting fact", "Numeric constraint", "API", "API parameter", "Feature", "Process"]
    qlabels = [
        ("数据库表/字段", "(Database, n=31)"),
        ("规格条款约束", "(Constraint, n=16)"),
        ("会议纪要事实", "(Meeting, n=9)"),
        ("数值指标约束", "(Numeric, n=9)"),
        ("接口定义检索", "(API, n=17)"),
        ("接口参数核查", "(API Param, n=10)"),
        ("功能模块概述", "(Feature, n=23)"),
        ("跨句业务流程", "(Process, n=10)"),
    ]
    W, H = 980, 440
    left, right, top, bottom = 75, 950, 65, 360
    plot_w = right - left
    plot_h = bottom - top

    svg = [
        f'<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" viewBox="0 0 {W} {H}" style="background:#ffffff;font-family:\'PingFang SC\',\'Heiti SC\',\'Microsoft YaHei\',sans-serif;">',
        f'<text x="{W/2}" y="28" text-anchor="middle" font-size="16" font-weight="bold" fill="#1a1a1a">图 4-1 锁定测试集不同智能体问题类型在各父子融合权重 (α) 下的检索精度 (Unit MRR)</text>',
    ]
    # Y grid
    for y_val in [0.0, 0.2, 0.4, 0.6, 0.8, 1.0]:
        y_px = bottom - (y_val / 1.1) * plot_h
        svg.append(f'<line x1="{left}" y1="{y_px:.1f}" x2="{right}" y2="{y_px:.1f}" stroke="#e0e0e0" stroke-dasharray="3,3"/>')
        svg.append(f'<text x="{left-10}" y="{y_px+4:.1f}" text-anchor="end" font-size="11" fill="#444">{y_val:.1f}</text>')

    svg.append(f'<line x1="{left}" y1="{top}" x2="{left}" y2="{bottom}" stroke="#333" stroke-width="1.2"/>')
    svg.append(f'<line x1="{left}" y1="{bottom}" x2="{right}" y2="{bottom}" stroke="#333" stroke-width="1.2"/>')
    svg.append(f'<text x="22" y="{(top+bottom)/2}" text-anchor="middle" font-size="12" fill="#222" transform="rotate(-90 22 {(top+bottom)/2})">单元级平均倒数排名 (Unit MRR)</text>')

    group_w = plot_w / len(qtypes)
    bar_w = 22
    colors = [("#2b5c8f", "α=0.0 (纯子块打分)"), ("#d95f02", "α=0.35 (父子融合打分)"), ("#7570b3", "α=1.0 (纯父块打分)")]

    for idx, qt in enumerate(qtypes):
        sub = [r for r in locked_rows if r["question_type"] == qt]
        vals = [
            sum(1.0 / r["rank_alpha_0_0"] if r["rank_alpha_0_0"] else 0.0 for r in sub) / len(sub),
            sum(1.0 / r["rank_alpha_0_35"] if r["rank_alpha_0_35"] else 0.0 for r in sub) / len(sub),
            sum(1.0 / r["rank_alpha_1_0"] if r["rank_alpha_1_0"] else 0.0 for r in sub) / len(sub),
        ]
        cx = left + (idx + 0.5) * group_w
        for b_idx, (val, (col, _)) in enumerate(zip(vals, colors)):
            bx = cx + (b_idx - 1.5) * bar_w + 2
            bh = (val / 1.1) * plot_h
            by = bottom - bh
            svg.append(f'<rect x="{bx:.1f}" y="{by:.1f}" width="{bar_w-2}" height="{bh:.1f}" fill="{col}" rx="1.5"/>')
            svg.append(f'<text x="{bx + (bar_w-2)/2:.1f}" y="{by-4:.1f}" text-anchor="middle" font-size="9.5" fill="#222">{val:.2f}</text>')
        l1, l2 = qlabels[idx]
        svg.append(f'<text x="{cx:.1f}" y="{bottom+20}" text-anchor="middle" font-size="11" fill="#222">{_svg_escape(l1)}</text>')
        svg.append(f'<text x="{cx:.1f}" y="{bottom+36}" text-anchor="middle" font-size="10" fill="#555">{_svg_escape(l2)}</text>')

    # Dividers between role layers
    div1 = left + 4 * group_w
    div2 = left + 6 * group_w
    svg.append(f'<line x1="{div1:.1f}" y1="{top}" x2="{div1:.1f}" y2="{bottom}" stroke="#666" stroke-dasharray="5,4" stroke-width="1.2"/>')
    svg.append(f'<line x1="{div2:.1f}" y1="{top}" x2="{div2:.1f}" y2="{bottom}" stroke="#666" stroke-dasharray="5,4" stroke-width="1.2"/>')
    svg.append(f'<text x="{left + 2*group_w:.1f}" y="{top-8}" text-anchor="middle" font-size="11.5" font-weight="bold" fill="#2b5c8f">执行层智能体：精确事实型需求 (α=0.0 最优)</text>')
    svg.append(f'<text x="{left + 5*group_w:.1f}" y="{top-8}" text-anchor="middle" font-size="11.5" font-weight="bold" fill="#d95f02">模块层：层次上下文型 (α=0.35 最优)</text>')
    svg.append(f'<text x="{left + 7*group_w:.1f}" y="{top-8}" text-anchor="middle" font-size="11.5" font-weight="bold" fill="#444">协调/章节层：概述型</text>')

    # Legend
    lx = left + 160
    for col, lbl in colors:
        svg.append(f'<rect x="{lx}" y="{H-26}" width="14" height="12" fill="{col}" rx="2"/>')
        svg.append(f'<text x="{lx+20}" y="{H-16}" font-size="11.5" fill="#222">{_svg_escape(lbl)}</text>')
        lx += 210

    svg.append("</svg>")
    (FIG_DIR / "fig4_1_question_type_mrr.svg").write_text("\n".join(svg), encoding="utf-8")

    # 2. fig4_2_routing_comparison.svg
    W, H = 940, 420
    svg2 = [
        f'<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" viewBox="0 0 {W} {H}" style="background:#ffffff;font-family:\'PingFang SC\',\'Heiti SC\',\'Microsoft YaHei\',sans-serif;">',
        f'<text x="{W/2}" y="28" text-anchor="middle" font-size="16" font-weight="bold" fill="#1a1a1a">图 4-2 全局固定配置与角色感知动态路由配置 (MAS-DRG) 的检索性能对比</text>',
    ]
    cfg_keys = [
        ("seac_parent_only_alpha_1.0", "仅父块检索", "(α=1.0)"),
        ("seac_default_alpha_0.35", "全局默认融合", "(α=0.35)"),
        ("seac_no_fusion_alpha_0.0_parent_return", "全局仅子块", "(α=0.0)"),
        ("mas_dynamic_recall_oriented", "角色动态路由 I", "(召回优先)"),
        ("mas_dynamic_precision_oriented", "角色动态路由 II", "(精度优先)"),
    ]
    for p_idx, (split_key, panel_title, p_left, p_right) in enumerate([
        ("dev_val", "(a) 开发与验证集 (P01–P03, N=83)", 65, 455),
        ("locked_test", "(b) 锁定测试集 (P04–P09, N=125)", 525, 915),
    ]):
        p_top, p_bot = 70, 340
        pw, ph = p_right - p_left, p_bot - p_top
        svg2.append(f'<text x="{(p_left+p_right)/2}" y="54" text-anchor="middle" font-size="13" font-weight="bold" fill="#222">{_svg_escape(panel_title)}</text>')
        for y_val in [0.5, 0.6, 0.7, 0.8, 0.9, 1.0]:
            y_px = p_bot - ((y_val - 0.5) / 0.55) * ph
            svg2.append(f'<line x1="{p_left}" y1="{y_px:.1f}" x2="{p_right}" y2="{y_px:.1f}" stroke="#e0e0e0" stroke-dasharray="3,3"/>')
            svg2.append(f'<text x="{p_left-8}" y="{y_px+4:.1f}" text-anchor="end" font-size="10.5" fill="#444">{y_val:.1f}</text>')
        svg2.append(f'<line x1="{p_left}" y1="{p_top}" x2="{p_left}" y2="{p_bot}" stroke="#333" stroke-width="1.2"/>')
        svg2.append(f'<line x1="{p_left}" y1="{p_bot}" x2="{p_right}" y2="{p_bot}" stroke="#333" stroke-width="1.2"/>')

        gw = pw / len(cfg_keys)
        bw = 22
        for c_idx, (ckey, l1, l2) in enumerate(cfg_keys):
            c = summary[split_key]["configurations"][ckey]
            mrr, h5 = c["mrr"], c["hit@5"]
            cx = p_left + (c_idx + 0.5) * gw
            for b_i, (v, col) in enumerate([(mrr, "#2b5c8f"), (h5, "#d95f02")]):
                bx = cx + (b_i - 1) * bw + 1
                bh = ((v - 0.5) / 0.55) * ph
                by = p_bot - bh
                svg2.append(f'<rect x="{bx:.1f}" y="{by:.1f}" width="{bw-2}" height="{bh:.1f}" fill="{col}" rx="1.5"/>')
                svg2.append(f'<text x="{bx+(bw-2)/2:.1f}" y="{by-4:.1f}" text-anchor="middle" font-size="8.8" fill="#222">{v:.4f}</text>')
            svg2.append(f'<text x="{cx:.1f}" y="{p_bot+18}" text-anchor="middle" font-size="10" fill="#222">{_svg_escape(l1)}</text>')
            svg2.append(f'<text x="{cx:.1f}" y="{p_bot+32}" text-anchor="middle" font-size="9.5" fill="#555">{_svg_escape(l2)}</text>')

    svg2.append(f'<rect x="360" y="{H-24}" width="14" height="12" fill="#2b5c8f" rx="2"/>')
    svg2.append(f'<text x="380" y="{H-14}" font-size="11.5" fill="#222">单元级平均倒数排名 (Unit MRR)</text>')
    svg2.append(f'<rect x="570" y="{H-24}" width="14" height="12" fill="#d95f02" rx="2"/>')
    svg2.append(f'<text x="590" y="{H-14}" font-size="11.5" fill="#222">前 5 命中率 (Hit@5)</text>')
    svg2.append("</svg>")
    (FIG_DIR / "fig4_2_routing_comparison.svg").write_text("\n".join(svg2), encoding="utf-8")

    # 3. fig4_3_token_accuracy_tradeoff.svg
    W, H = 820, 440
    left, right, top, bottom = 80, 770, 60, 365
    pw, ph = right - left, bottom - top
    svg3 = [
        f'<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" viewBox="0 0 {W} {H}" style="background:#ffffff;font-family:\'PingFang SC\',\'Heiti SC\',\'Microsoft YaHei\',sans-serif;">',
        f'<text x="{W/2}" y="28" text-anchor="middle" font-size="16" font-weight="bold" fill="#1a1a1a">图 4-3 锁定测试集不同策略的检索精度 (Unit MRR) 与上下文 Token 成本权衡</text>',
    ]
    for x_val in [200, 600, 1000, 1400, 1800, 2200, 2600]:
        x_px = left + ((x_val - 150) / 2550) * pw
        svg3.append(f'<line x1="{x_px:.1f}" y1="{top}" x2="{x_px:.1f}" y2="{bottom}" stroke="#e5e5e5" stroke-dasharray="3,3"/>')
        svg3.append(f'<text x="{x_px:.1f}" y="{bottom+18}" text-anchor="middle" font-size="10.5" fill="#444">{x_val}</text>')
    for y_val in [0.50, 0.60, 0.70, 0.80, 0.85]:
        y_px = bottom - ((y_val - 0.48) / 0.40) * ph
        svg3.append(f'<line x1="{left}" y1="{y_px:.1f}" x2="{right}" y2="{y_px:.1f}" stroke="#e5e5e5" stroke-dasharray="3,3"/>')
        svg3.append(f'<text x="{left-10}" y="{y_px+4:.1f}" text-anchor="end" font-size="10.5" fill="#444">{y_val:.2f}</text>')

    svg3.append(f'<line x1="{left}" y1="{top}" x2="{left}" y2="{bottom}" stroke="#333" stroke-width="1.2"/>')
    svg3.append(f'<line x1="{left}" y1="{bottom}" x2="{right}" y2="{bottom}" stroke="#333" stroke-width="1.2"/>')
    svg3.append(f'<text x="{(left+right)/2}" y="{H-18}" text-anchor="middle" font-size="12" fill="#222">单次查询平均返回上下文 Token 开销 (Top-5 块总 Token 数)</text>')
    svg3.append(f'<text x="24" y="{(top+bottom)/2}" text-anchor="middle" font-size="12" fill="#222" transform="rotate(-90 24 {(top+bottom)/2})">锁定测试集 Unit MRR</text>')

    pts = [
        ("fixed_window", "固定窗口切片 (Fixed-512)", "#7f7f7f", -110, -10),
        ("seac_parent_only_alpha_1.0", "SEAC 仅父块检索 (α=1.0, 父块返回)", "#7570b3", 12, 4),
        ("seac_default_alpha_0.35", "SEAC 默认全局配置 (α=0.35, 父块返回)", "#d95f02", 12, 4),
        ("seac_no_fusion_alpha_0.0_parent_return", "SEAC 全局子块打分 (α=0.0, 父块返回)", "#2b5c8f", 12, 8),
        ("seac_no_fusion_alpha_0.0_child_return", "SEAC 全局子块打分 (α=0.0, 仅子块返回)", "#66c2a5", 12, 12),
        ("mas_dynamic_recall_oriented", "角色动态路由 I (召回优先, 按需返回, Hit@5=0.960)", "#e7298a", 12, 14),
        ("mas_dynamic_precision_oriented", "角色动态路由 II (精度优先, 按需返回, MRR=0.8416)", "#1b9e77", 12, -10),
    ]
    for key, lbl, col, dx, dy in pts:
        c = summary["locked_test"]["configurations"][key]
        tok, mrr = c["avg_top5_tokens"], c["mrr"]
        px = left + ((tok - 150) / 2550) * pw
        py = bottom - ((mrr - 0.48) / 0.40) * ph
        r_sz = 7.5 if "mas_dynamic" in key else 6.0
        svg3.append(f'<circle cx="{px:.1f}" cy="{py:.1f}" r="{r_sz}" fill="{col}" stroke="#fff" stroke-width="1.5"/>')
        svg3.append(f'<text x="{px+dx:.1f}" y="{py+dy:.1f}" font-size="10.5" font-weight="{"bold" if "mas_dynamic" in key else "normal"}" fill="{col}">{_svg_escape(lbl)} [{tok:.0f} tok, MRR={mrr:.4f}]</text>')

    svg3.append("</svg>")
    (FIG_DIR / "fig4_3_token_accuracy_tradeoff.svg").write_text("\n".join(svg3), encoding="utf-8")


def main():
    proj_tokens = load_project_token_stats()
    all_rows = load_all_questions(proj_tokens)

    csv_path = RESULTS_DIR / "chapter4_questions_role_mapping.csv"
    with csv_path.open("w", encoding="utf-8-sig", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=list(all_rows[0].keys()))
        writer.writeheader()
        writer.writerows(all_rows)

    summary = {"project_token_stats": proj_tokens}
    for split_key in ("dev_val", "locked_test"):
        sub_rows = [r for r in all_rows if r["split"] == split_key]
        by_qtype = {}
        for qt in sorted({r["question_type"] for r in sub_rows}):
            q_rows = [r for r in sub_rows if r["question_type"] == qt]
            by_qtype[qt] = {
                "count": len(q_rows),
                "agent_role": ROLE_MAPPING[qt]["agent_role"],
                "agent_role_cn": ROLE_MAPPING[qt]["agent_role_cn"],
                "demand_type_cn": ROLE_MAPPING[qt]["demand_type_cn"],
                "alpha_0.0": metrics_from_ranks([r["rank_alpha_0_0"] for r in q_rows], [r["span_rank_alpha_0_0"] for r in q_rows], [r["child_tokens_top1"] for r in q_rows]),
                "alpha_0.35": metrics_from_ranks([r["rank_alpha_0_35"] for r in q_rows], [r["span_rank_alpha_0_35"] for r in q_rows], [r["parent_tokens_top1"] for r in q_rows]),
                "alpha_1.0": metrics_from_ranks([r["rank_alpha_1_0"] for r in q_rows], [r["span_rank_alpha_1_0"] for r in q_rows], [r["parent_tokens_top1"] for r in q_rows]),
            }

        by_demand = {}
        for dt in ("atomic_fact", "hierarchical_context", "macro_overview"):
            d_rows = [r for r in sub_rows if r["demand_type"] == dt]
            by_demand[dt] = {
                "count": len(d_rows),
                "alpha_0.0": metrics_from_ranks([r["rank_alpha_0_0"] for r in d_rows], [r["span_rank_alpha_0_0"] for r in d_rows], [r["child_tokens_top1"] for r in d_rows]),
                "alpha_0.35": metrics_from_ranks([r["rank_alpha_0_35"] for r in d_rows], [r["span_rank_alpha_0_35"] for r in d_rows], [r["parent_tokens_top1"] for r in d_rows]),
                "alpha_1.0": metrics_from_ranks([r["rank_alpha_1_0"] for r in d_rows], [r["span_rank_alpha_1_0"] for r in d_rows], [r["parent_tokens_top1"] for r in d_rows]),
            }

        summary[split_key] = {
            "total_questions": len(sub_rows),
            "by_question_type": by_qtype,
            "by_demand_type": by_demand,
            "configurations": evaluate_configurations(sub_rows),
        }

    (RESULTS_DIR / "chapter4_summary.json").write_text(json.dumps(summary, ensure_ascii=False, indent=2), encoding="utf-8")
    generate_svg_figures(all_rows, summary)
    print("Successfully generated Chapter 4 summary JSON, CSV, and SVG figures.")


if __name__ == "__main__":
    main()
