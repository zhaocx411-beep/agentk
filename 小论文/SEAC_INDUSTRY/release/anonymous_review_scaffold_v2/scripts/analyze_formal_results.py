from __future__ import annotations

import argparse
import csv
import hashlib
import json
import random
from collections import defaultdict
from pathlib import Path


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def percentile(values: list[float], q: float) -> float:
    if not values:
        return 0.0
    ordered = sorted(values)
    position = (len(ordered) - 1) * q
    lower = int(position)
    upper = min(lower + 1, len(ordered) - 1)
    fraction = position - lower
    return ordered[lower] * (1 - fraction) + ordered[upper] * fraction


def retrieval_summary(rows: list[dict]) -> dict:
    count = len(rows)
    ranks = [row.get("rank") for row in rows]
    summary = {
        "question_count": count,
        "mrr": sum(1 / rank if rank else 0.0 for rank in ranks) / count if count else 0.0,
    }
    for k in (1, 3, 5):
        summary[f"hit_rate@{k}"] = (
            sum(bool(rank and rank <= k) for rank in ranks) / count if count else 0.0
        )
    span_rows = [row for row in rows if row.get("has_gold_span")]
    if span_rows:
        span_ranks = [row.get("span_rank") for row in span_rows]
        summary.update(
            {
                "span_question_count": len(span_rows),
                "span_mrr": sum(1 / rank if rank else 0.0 for rank in span_ranks) / len(span_rows),
                **{
                    f"span_hit_rate@{k}": sum(bool(rank and rank <= k) for rank in span_ranks) / len(span_rows)
                    for k in (1, 3, 5)
                },
            }
        )
    return summary


def aggregate_efficiency(rows: list[dict]) -> dict:
    by_method = defaultdict(list)
    for row in rows:
        by_method[row["method"]].append(row)
    output = {}
    for method, items in sorted(by_method.items()):
        question_count = sum(int(item["question_count"]) for item in items)
        query_latencies = [
            float(value)
            for item in items
            for value in item.get("query_latencies_ms", [])
        ]
        if query_latencies and len(query_latencies) != question_count:
            raise ValueError(
                f"{method}: expected {question_count} query latencies, got {len(query_latencies)}"
            )
        if query_latencies:
            query_latency_mean = sum(query_latencies) / len(query_latencies)
            query_latency_p50 = percentile(query_latencies, 0.50)
            query_latency_p95 = percentile(query_latencies, 0.95)
        else:
            # Backward-compatible fallback for exploratory summaries created before v1.1.
            query_latency_mean = (
                sum(float(item["query_latency_ms_mean"]) * int(item["question_count"]) for item in items)
                / question_count
                if question_count
                else 0.0
            )
            query_latency_p50 = 0.0
            query_latency_p95 = 0.0
        output[method] = {
            "project_count": len({item["project_id"] for item in items}),
            "question_count": question_count,
            "parent_chunk_count": sum(int(item["parent_chunk_count"]) for item in items),
            "retrieval_vector_count": sum(int(item["retrieval_vector_count"]) for item in items),
            "chunking_seconds": sum(float(item["chunking_seconds"]) for item in items),
            "index_seconds": sum(float(item["index_seconds"]) for item in items),
            "query_latency_ms_mean": query_latency_mean,
            "query_latency_ms_p50": query_latency_p50,
            "query_latency_ms_p95": query_latency_p95,
            "by_project": {
                item["project_id"]: {
                    key: value
                    for key, value in item.items()
                    if key not in {"method", "project_id"}
                }
                for item in sorted(items, key=lambda value: value["project_id"])
            },
        }
    return output


def boundary_summary(rows: list[dict]) -> dict:
    tp = sum(row["human_label"] == "BREAK" and row["predicted_label"] == "BREAK" for row in rows)
    fp = sum(row["human_label"] == "NO_BREAK" and row["predicted_label"] == "BREAK" for row in rows)
    fn = sum(row["human_label"] == "BREAK" and row["predicted_label"] == "NO_BREAK" for row in rows)
    tn = sum(row["human_label"] == "NO_BREAK" and row["predicted_label"] == "NO_BREAK" for row in rows)
    precision = tp / (tp + fp) if tp + fp else 0.0
    recall = tp / (tp + fn) if tp + fn else 0.0
    return {
        "candidate_count": len(rows),
        "tp": tp,
        "fp": fp,
        "fn": fn,
        "tn": tn,
        "precision": precision,
        "recall": recall,
        "f1": 2 * precision * recall / (precision + recall) if precision + recall else 0.0,
        "accuracy": (tp + tn) / len(rows) if rows else 0.0,
    }


def grouped(rows: list[dict], key: str, summarizer) -> dict:
    buckets = defaultdict(list)
    for row in rows:
        buckets[str(row.get(key) or "UNKNOWN")].append(row)
    return {name: summarizer(items) for name, items in sorted(buckets.items())}


def cluster_bootstrap(
    rows: list[dict],
    statistic,
    iterations: int,
    seed: int,
    cluster_key: str = "project_id",
) -> tuple[float, float]:
    by_cluster = defaultdict(list)
    for row in rows:
        by_cluster[str(row.get(cluster_key) or "UNKNOWN")].append(row)
    cluster_names = sorted(by_cluster)
    if not cluster_names or iterations <= 0:
        return 0.0, 0.0
    rng = random.Random(seed)
    estimates = []
    for _ in range(iterations):
        sample = []
        for name in rng.choices(cluster_names, k=len(cluster_names)):
            cluster = by_cluster[name]
            sample.extend(rng.choices(cluster, k=len(cluster)))
        estimates.append(statistic(sample))
    return percentile(estimates, 0.025), percentile(estimates, 0.975)


def metric_value(row: dict, metric: str) -> float:
    if metric == "mrr":
        rank = row.get("rank")
        return 1 / rank if rank else 0.0
    if metric.startswith("hit_rate@"):
        k = int(metric.split("@", 1)[1])
        rank = row.get("rank")
        return float(bool(rank and rank <= k))
    if metric == "span_mrr":
        rank = row.get("span_rank")
        return 1 / rank if rank else 0.0
    if metric.startswith("span_hit_rate@"):
        k = int(metric.split("@", 1)[1])
        rank = row.get("span_rank")
        return float(bool(rank and rank <= k))
    raise ValueError(f"Unknown retrieval metric: {metric}")


def clustered_sign_flip_pvalue(
    pairs: list[dict], iterations: int, seed: int, cluster_key: str = "project_id"
) -> dict:
    """Two-sided paired test that flips every observation in a project together."""

    if not pairs:
        return {
            "p_value": 1.0,
            "cluster_count": 0,
            "randomization_unit": cluster_key,
            "randomization_mode": "none",
            "randomization_permutations": 0,
        }
    by_cluster = defaultdict(list)
    for item in pairs:
        by_cluster[str(item.get(cluster_key) or "UNKNOWN")].append(float(item["difference"]))
    cluster_names = sorted(by_cluster)
    observed = abs(sum(item["difference"] for item in pairs) / len(pairs))
    cluster_sums = [sum(by_cluster[name]) for name in cluster_names]
    if len(cluster_names) <= 20:
        permutation_count = 1 << len(cluster_names)
        extreme = 0
        for mask in range(permutation_count):
            estimate = abs(
                sum(
                    value if mask & (1 << index) else -value
                    for index, value in enumerate(cluster_sums)
                )
                / len(pairs)
            )
            extreme += estimate >= observed - 1e-15
        p_value = extreme / permutation_count
        mode = "exact_cluster_enumeration"
    else:
        rng = random.Random(seed)
        extreme = 0
        for _ in range(iterations):
            estimate = abs(
                sum(value if rng.random() < 0.5 else -value for value in cluster_sums)
                / len(pairs)
            )
            extreme += estimate >= observed - 1e-15
        permutation_count = iterations
        p_value = (extreme + 1) / (iterations + 1)
        mode = "monte_carlo_cluster_sign_flip"
    return {
        "p_value": p_value,
        "cluster_count": len(cluster_names),
        "randomization_unit": cluster_key,
        "randomization_mode": mode,
        "randomization_permutations": permutation_count,
    }


def holm_adjust(p_values: list[float]) -> list[float]:
    indexed = sorted(enumerate(p_values), key=lambda item: item[1])
    adjusted = [1.0] * len(p_values)
    running = 0.0
    total = len(p_values)
    for rank, (index, value) in enumerate(indexed):
        running = max(running, min(1.0, (total - rank) * value))
        adjusted[index] = running
    return adjusted


def resolve_input(root: Path, value: str) -> Path:
    path = Path(value)
    return path if path.is_absolute() else root / path


FROZEN_PARAMETER_MAP = {
    "max_tokens": "max_tokens",
    "overlap": "overlap",
    "semantic_percentile": "semantic_percentile",
    "semantic_weight": "semantic_weight",
    "structure_weight": "structure_weight",
    "density_weight": "key_density_change_weight",
    "continuity_penalty": "same_heading_continuity_penalty",
    "protected_group_penalty": "same_protected_group_penalty",
    "structure_heading_signal": "structure_heading_signal",
    "structure_path_change_signal": "structure_path_change_signal",
    "structure_group_change_signal": "structure_group_change_signal",
    "density_change_scale": "density_change_scale",
    "table_ratio_threshold_adjustment": "table_ratio_threshold_adjustment",
    "heading_ratio_threshold_adjustment": "heading_ratio_threshold_adjustment",
    "key_density_cutoff": "key_density_cutoff",
    "proposed_soft_target": "parent_soft_target_tokens",
    "proposed_dense_target": "parent_dense_target_tokens",
    "proposed_min_chunk": "parent_min_tokens",
    "v6_child_target": "child_target_tokens",
    "v6_child_max": "child_max_tokens",
    "hierarchy_parent_weight": "hierarchy_parent_weight",
    "fair_context": "fair_context",
}


def validate_config(
    config: dict,
    root: Path,
    allow_exploratory: bool,
    frozen: dict | None = None,
    frozen_path: Path | None = None,
) -> list[str]:
    issues = []
    if not config.get("formal_run"):
        issues.append("formal_run is not true")
    if config.get("run_status") != "COMPLETE":
        issues.append("run_status is not COMPLETE")
    if config.get("allow_unreviewed_qa"):
        issues.append("unreviewed QA was allowed")
    if config.get("allow_provisional_breakpoints"):
        issues.append("provisional breakpoints were allowed")
    if config.get("semantic_encoder") != "bge-m3":
        issues.append("semantic_encoder is not bge-m3")
    if config.get("evidence_mode") != "unit":
        issues.append("evidence_mode is not unit")
    if frozen is not None:
        for config_key, frozen_key in FROZEN_PARAMETER_MAP.items():
            if config.get(config_key) != frozen.get(frozen_key):
                issues.append(
                    f"{config_key}={config.get(config_key)!r} differs from frozen {frozen_key}={frozen.get(frozen_key)!r}"
                )
        protocol_value = config.get("protocol_config")
        protocol_hash = config.get("protocol_config_sha256")
        if not protocol_value or not protocol_hash:
            issues.append("frozen protocol path or hash is missing")
        else:
            protocol_path = resolve_input(root, protocol_value)
            if not protocol_path.is_file():
                issues.append(f"frozen protocol file is missing: {protocol_path}")
            elif sha256(protocol_path) != protocol_hash:
                issues.append("frozen protocol SHA-256 does not match the run config")
            if frozen_path is not None and protocol_path.resolve() != frozen_path.resolve():
                issues.append("run used a different frozen protocol path")
            if frozen_path is not None and protocol_hash != sha256(frozen_path):
                issues.append("run used a different frozen protocol content hash")
    # A withheld axis records no path and no hash on purpose. The manifest is where
    # that omission is declared and justified, so it is not an issue here.
    withheld_flag = {"qa": "retrieval_withheld", "breakpoints": "boundary_withheld"}
    for key in ("corpus", "qa", "breakpoints"):
        if config.get(withheld_flag.get(key, "")):
            continue
        expected = config.get(f"{key}_sha256")
        value = config.get(key)
        if not value or not expected:
            issues.append(f"{key} path or hash is missing")
            continue
        path = resolve_input(root, value)
        if not path.is_file():
            issues.append(f"{key} file is missing: {path}")
        elif sha256(path) != expected:
            issues.append(f"{key} SHA-256 does not match")
    if issues and not allow_exploratory:
        raise ValueError("Formal run validation failed: " + "; ".join(issues))
    return issues


def load_run(
    run_dir: Path,
    root: Path,
    allow_exploratory: bool,
    frozen: dict | None = None,
    frozen_path: Path | None = None,
) -> tuple[dict, list[dict], list[dict], list[dict], list[str]]:
    config = json.loads((run_dir / "config.json").read_text(encoding="utf-8"))
    results = json.loads((run_dir / "results.json").read_text(encoding="utf-8"))
    issues = validate_config(config, root, allow_exploratory, frozen, frozen_path)
    # A project withheld from retrieval reporting contributes boundary rows only.
    # Its questions are never loaded, so they cannot reach the pooled metrics.
    retrieval_withheld = bool(config.get("retrieval_withheld")) or config.get("qa") is None
    questions = []
    if not retrieval_withheld:
        qa_path = resolve_input(root, config["qa"])
        questions = json.loads(qa_path.read_text(encoding="utf-8"))
    qa_gate_issues = []
    for item in questions:
        qa_id = item.get("qa_id", "<unknown>")
        statuses = {
            str(item.get("audit_status", "")).strip().upper(),
            str(item.get("review_status", "")).strip().upper(),
        }
        if not statuses.intersection({"KEEP", "APPROVED", "VERIFIED", "已复核", "已确认", "已仲裁"}):
            qa_gate_issues.append(f"{qa_id}: QA is not approved")
        if str(item.get("privacy_review_status", "")).strip().upper() not in {"CLEARED", "NO_RISK", "ANONYMIZED"}:
            qa_gate_issues.append(f"{qa_id}: privacy is not cleared")
        if not (item.get("gold_evidence_unit_id") or item.get("gold_evidence_unit_ids")):
            qa_gate_issues.append(f"{qa_id}: exact evidence unit is missing")
        if not (item.get("gold_evidence_span") or item.get("gold_evidence_spans")):
            qa_gate_issues.append(f"{qa_id}: exact evidence span is missing")
    if qa_gate_issues and not allow_exploratory:
        raise ValueError("Formal QA validation failed: " + "; ".join(qa_gate_issues[:20]))
    issues.extend(qa_gate_issues)
    project_id = str(config.get("project_id") or "").strip()
    if not project_id:
        project_ids = {item.get("project_id") for item in questions if item.get("project_id")}
        if len(project_ids) != 1:
            raise ValueError(
                f"{run_dir}: config.json records no project_id and the questions do not "
                f"identify exactly one project: {sorted(project_ids)}"
            )
        project_id = next(iter(project_ids))
    qa_lookup = {item["qa_id"]: item for item in questions}
    retrieval_rows = []
    boundary_rows = []
    efficiency_rows = []
    for method in config["methods"]:
        if method not in results:
            raise ValueError(f"{run_dir}: method {method} is missing from results.json")
        if retrieval_withheld:
            boundary_path = run_dir / f"{method}_boundary_details.json"
            if boundary_path.is_file():
                for item in json.loads(boundary_path.read_text(encoding="utf-8")):
                    boundary_rows.append({**item, "project_id": project_id, "method": method})
            continue
        detail_path = run_dir / f"{method}_retrieval_details.json"
        details = json.loads(detail_path.read_text(encoding="utf-8"))
        missing_latencies = [
            item.get("qa_id", "<unknown>")
            for item in details
            if item.get("query_latency_ms") is None
        ]
        if missing_latencies:
            message = (
                f"{method}: {len(missing_latencies)} retrieval details lack query_latency_ms "
                f"(examples: {missing_latencies[:5]})"
            )
            if not allow_exploratory:
                raise ValueError(message)
            issues.append(message)
        method_result = results[method]
        retrieval_result = method_result["retrieval"]
        chunk_count = int(method_result["chunk_count"])
        efficiency_rows.append(
            {
                "project_id": project_id,
                "method": method,
                "question_count": len(details),
                "parent_chunk_count": int(method_result.get("parent_chunk_count", chunk_count)),
                # evaluate_retrieval indexes every emitted chunk for hierarchical methods.
                "retrieval_vector_count": chunk_count,
                "chunking_seconds": float(method_result["chunking_seconds"]),
                "index_seconds": float(retrieval_result["index_seconds"]),
                "query_latency_ms_mean": float(retrieval_result["query_latency_ms_mean"]),
                "query_latencies_ms": [
                    float(item["query_latency_ms"])
                    for item in details
                    if item.get("query_latency_ms") is not None
                ],
            }
        )
        for item in details:
            qa = qa_lookup[item["qa_id"]]
            retrieval_rows.append(
                {
                    **item,
                    "project_id": project_id,
                    "method": method,
                    "question_type": item.get("question_type") or qa.get("question_type", "UNKNOWN"),
                    "source_document": item.get("source_document") or qa.get("source_document", ""),
                    "has_gold_span": item.get("has_gold_span", bool(qa.get("gold_evidence_span") or qa.get("gold_evidence_spans"))),
                }
            )
        boundary_path = run_dir / f"{method}_boundary_details.json"
        if boundary_path.is_file():
            for item in json.loads(boundary_path.read_text(encoding="utf-8")):
                boundary_rows.append({**item, "project_id": project_id, "method": method})
    return config, retrieval_rows, boundary_rows, efficiency_rows, issues


def attach_retrieval_confidence(summary: dict, rows: list[dict], iterations: int, seed: int) -> None:
    metrics = ["mrr", "hit_rate@1", "hit_rate@3", "hit_rate@5"]
    if any(row.get("has_gold_span") for row in rows):
        metrics.extend(["span_mrr", "span_hit_rate@1", "span_hit_rate@3", "span_hit_rate@5"])
    for offset, metric in enumerate(metrics):
        eligible = [row for row in rows if row.get("has_gold_span")] if metric.startswith("span_") else rows
        low, high = cluster_bootstrap(
            eligible,
            lambda sample, name=metric: sum(metric_value(row, name) for row in sample) / len(sample),
            iterations,
            seed + offset,
        )
        summary[f"{metric}_ci95"] = [low, high]


def paired_retrieval(primary_rows: list[dict], comparison_rows: list[dict], iterations: int, seed: int) -> list[dict]:
    primary = {(row["project_id"], row["qa_id"]): row for row in primary_rows}
    comparison = {(row["project_id"], row["qa_id"]): row for row in comparison_rows}
    if set(primary) != set(comparison):
        raise ValueError("Paired retrieval comparison has mismatched QA identifiers")
    output = []
    for offset, metric in enumerate(("mrr", "hit_rate@5", "span_mrr", "span_hit_rate@5")):
        pairs = []
        for key in sorted(primary):
            left, right = primary[key], comparison[key]
            if metric.startswith("span_") and not (left.get("has_gold_span") and right.get("has_gold_span")):
                continue
            pairs.append(
                {
                    "project_id": key[0],
                    "difference": metric_value(left, metric) - metric_value(right, metric),
                }
            )
        differences = [item["difference"] for item in pairs]
        estimate = sum(differences) / len(differences) if differences else 0.0
        low, high = cluster_bootstrap(
            pairs,
            lambda sample: sum(item["difference"] for item in sample) / len(sample),
            iterations,
            seed + 100 + offset,
        )
        randomization = clustered_sign_flip_pvalue(pairs, iterations, seed + 200 + offset)
        output.append(
            {
                "metric": metric,
                "pair_count": len(pairs),
                "mean_difference": estimate,
                "ci95": [low, high],
                **randomization,
            }
        )
    return output


def paired_boundary(primary_rows: list[dict], comparison_rows: list[dict], iterations: int, seed: int) -> dict:
    primary = {(row["project_id"], row["candidate_id"]): row for row in primary_rows}
    comparison = {(row["project_id"], row["candidate_id"]): row for row in comparison_rows}
    if set(primary) != set(comparison):
        raise ValueError("Paired boundary comparison has mismatched candidate identifiers")
    pairs = [
        {
            "project_id": key[0],
            "human_label": primary[key]["human_label"],
            "primary_prediction": primary[key]["predicted_label"],
            "comparison_prediction": comparison[key]["predicted_label"],
        }
        for key in sorted(primary)
    ]

    def f1_difference(sample):
        left = [
            {"human_label": item["human_label"], "predicted_label": item["primary_prediction"]}
            for item in sample
        ]
        right = [
            {"human_label": item["human_label"], "predicted_label": item["comparison_prediction"]}
            for item in sample
        ]
        return boundary_summary(left)["f1"] - boundary_summary(right)["f1"]

    estimate = f1_difference(pairs)
    low, high = cluster_bootstrap(pairs, f1_difference, iterations, seed)
    cluster_names = sorted({item["project_id"] for item in pairs})

    def permuted_difference(swap_by_project: dict[str, bool]) -> float:
        permuted = []
        for item in pairs:
            swap = swap_by_project[item["project_id"]]
            permuted.append(
                {
                    **item,
                    "primary_prediction": item["comparison_prediction"] if swap else item["primary_prediction"],
                    "comparison_prediction": item["primary_prediction"] if swap else item["comparison_prediction"],
                }
            )
        return f1_difference(permuted)

    if len(cluster_names) <= 20:
        permutation_count = 1 << len(cluster_names)
        extreme = 0
        for mask in range(permutation_count):
            swap_by_project = {
                project: bool(mask & (1 << index))
                for index, project in enumerate(cluster_names)
            }
            extreme += abs(permuted_difference(swap_by_project)) >= abs(estimate) - 1e-15
        p_value = extreme / permutation_count
        mode = "exact_cluster_enumeration"
    else:
        rng = random.Random(seed + 1)
        extreme = 0
        for _ in range(iterations):
            swap_by_project = {project: rng.random() < 0.5 for project in cluster_names}
            extreme += abs(permuted_difference(swap_by_project)) >= abs(estimate) - 1e-15
        permutation_count = iterations
        p_value = (extreme + 1) / (iterations + 1)
        mode = "monte_carlo_cluster_swap"
    return {
        "pair_count": len(pairs),
        "f1_difference": estimate,
        "ci95": [low, high],
        "p_value": p_value,
        "cluster_count": len(cluster_names),
        "randomization_unit": "project_id",
        "randomization_mode": mode,
        "randomization_permutations": permutation_count,
    }


def analyze(
    retrieval_rows: list[dict],
    boundary_rows: list[dict],
    primary: str,
    confirmatory_comparator: str,
    iterations: int,
    seed: int,
) -> dict:
    retrieval_by_method = defaultdict(list)
    for row in retrieval_rows:
        retrieval_by_method[row["method"]].append(row)
    if primary not in retrieval_by_method:
        raise ValueError(f"Primary method {primary!r} is absent")
    if confirmatory_comparator == primary or confirmatory_comparator not in retrieval_by_method:
        raise ValueError(f"Invalid confirmatory comparator {confirmatory_comparator!r}")
    retrieval = {}
    for offset, (method, rows) in enumerate(sorted(retrieval_by_method.items())):
        overall = retrieval_summary(rows)
        attach_retrieval_confidence(overall, rows, iterations, seed + offset * 20)
        by_project = grouped(rows, "project_id", retrieval_summary)
        macro_keys = ("mrr", "hit_rate@1", "hit_rate@3", "hit_rate@5", "span_mrr", "span_hit_rate@5")
        macro = {
            key: sum(item[key] for item in by_project.values() if key in item) / sum(key in item for item in by_project.values())
            for key in macro_keys
            if any(key in item for item in by_project.values())
        }
        retrieval[method] = {
            "overall_micro": overall,
            "overall_macro_project": macro,
            "by_project": by_project,
            "by_question_type": grouped(rows, "question_type", retrieval_summary),
        }

    comparisons = {}
    for offset, method in enumerate(sorted(retrieval_by_method)):
        if method == primary:
            continue
        comparisons[method] = paired_retrieval(
            retrieval_by_method[primary], retrieval_by_method[method], iterations, seed + offset * 500
        )
    retrieval_tests = [
        (method, item)
        for method in sorted(comparisons)
        for item in comparisons[method]
    ]
    secondary_retrieval_tests = [
        item
        for method, item in retrieval_tests
        if not (method == confirmatory_comparator and item["metric"] == "mrr")
    ]
    retrieval_adjusted = holm_adjust([item["p_value"] for item in secondary_retrieval_tests])
    for item, value in zip(secondary_retrieval_tests, retrieval_adjusted):
        item["p_value_holm_secondary"] = value
    for method, item in retrieval_tests:
        if method == confirmatory_comparator and item["metric"] == "mrr":
            item["confirmatory_test"] = True
            item["p_value_holm_secondary"] = None
        else:
            item["confirmatory_test"] = False

    boundary = {}
    boundary_by_method = defaultdict(list)
    for row in boundary_rows:
        boundary_by_method[row["method"]].append(row)
    for offset, (method, rows) in enumerate(sorted(boundary_by_method.items())):
        overall = boundary_summary(rows)
        low, high = cluster_bootstrap(rows, lambda sample: boundary_summary(sample)["f1"], iterations, seed + 5000 + offset)
        overall["f1_ci95"] = [low, high]
        boundary[method] = {
            "overall_micro": overall,
            "by_project": grouped(rows, "project_id", boundary_summary),
            "by_stratum": grouped(rows, "boundary_stratum", boundary_summary),
            "by_document_type": grouped(rows, "document_type", boundary_summary),
        }
    boundary_comparisons = {}
    if primary in boundary_by_method:
        for offset, method in enumerate(sorted(boundary_by_method)):
            if method == primary:
                continue
            boundary_comparisons[method] = paired_boundary(
                boundary_by_method[primary], boundary_by_method[method], iterations, seed + 7000 + offset * 100
            )
        adjusted = holm_adjust([boundary_comparisons[method]["p_value"] for method in sorted(boundary_comparisons)])
        for method, value in zip(sorted(boundary_comparisons), adjusted):
            boundary_comparisons[method]["p_value_holm"] = value
    return {
        "protocol_version": "industrial-seac-formal-analysis-v1.1",
        "primary_method": primary,
        "confirmatory_comparator": confirmatory_comparator,
        "confirmatory_metric": "mrr",
        "bootstrap_samples": iterations,
        "random_seed": seed,
        "randomization_unit": "project_id",
        "randomization_rule": "exact_cluster_enumeration_when_project_count_at_most_20",
        "retrieval_holm_family_size": len(secondary_retrieval_tests),
        "boundary_holm_family_size": len(boundary_comparisons),
        "retrieval": retrieval,
        "paired_retrieval_comparisons": comparisons,
        "boundary": boundary,
        "paired_boundary_comparisons": boundary_comparisons,
    }


def write_outputs(analysis: dict, output_dir: Path, validation: list[dict]) -> None:
    output_dir.mkdir(parents=True, exist_ok=True)
    payload = {"run_validation": validation, **analysis}
    (output_dir / "formal_analysis.json").write_text(
        json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8"
    )
    with (output_dir / "retrieval_summary.csv").open("w", encoding="utf-8-sig", newline="") as handle:
        writer = csv.writer(handle)
        writer.writerow(["method", "questions", "mrr", "mrr_ci_low", "mrr_ci_high", "hit@1", "hit@3", "hit@5", "span_mrr", "span_hit@5"])
        for method, item in sorted(analysis["retrieval"].items()):
            row = item["overall_micro"]
            writer.writerow(
                [method, row["question_count"], row["mrr"], *row["mrr_ci95"], row["hit_rate@1"], row["hit_rate@3"], row["hit_rate@5"], row.get("span_mrr", ""), row.get("span_hit_rate@5", "")]
            )
    lines = [
        "# SEAC formal statistical analysis",
        "",
        f'- Primary method: `{analysis["primary_method"]}`',
        f'- Analysis scope: `{analysis.get("analysis_scope", "unspecified")}`',
        f'- Projects: {", ".join(analysis.get("project_ids", [])) or "unspecified"}',
        f'- Hierarchical bootstrap samples: {analysis["bootstrap_samples"]}',
        f'- Paired randomization unit: `{analysis["randomization_unit"]}`',
        f'- Confirmatory comparison: `{analysis["primary_method"]}` vs '
        f'`{analysis["confirmatory_comparator"]}` on `{analysis["confirmatory_metric"]}`',
        f'- Retrieval Holm family size: {analysis["retrieval_holm_family_size"]}',
        f'- Boundary Holm family size: {analysis["boundary_holm_family_size"]}',
        f'- Seed: {analysis["random_seed"]}',
        "",
        "## Retrieval (micro average)",
        "",
        "| Method | N | MRR (95% CI) | Hit@1 | Hit@3 | Hit@5 | Span MRR | Span Hit@5 |",
        "|---|---:|---:|---:|---:|---:|---:|---:|",
    ]
    for method, item in sorted(analysis["retrieval"].items()):
        row = item["overall_micro"]
        low, high = row["mrr_ci95"]
        lines.append(
            f'| {method} | {row["question_count"]} | {row["mrr"]:.4f} [{low:.4f}, {high:.4f}] | '
            f'{row["hit_rate@1"]:.4f} | {row["hit_rate@3"]:.4f} | {row["hit_rate@5"]:.4f} | '
            f'{row.get("span_mrr", 0.0):.4f} | {row.get("span_hit_rate@5", 0.0):.4f} |'
        )
    if analysis["boundary"]:
        lines.extend(["", "## Boundary detection", "", "| Method | N | Precision | Recall | F1 (95% CI) |", "|---|---:|---:|---:|---:|"])
        for method, item in sorted(analysis["boundary"].items()):
            row = item["overall_micro"]
            low, high = row["f1_ci95"]
            lines.append(f'| {method} | {row["candidate_count"]} | {row["precision"]:.4f} | {row["recall"]:.4f} | {row["f1"]:.4f} [{low:.4f}, {high:.4f}] |')
    if analysis.get("efficiency"):
        lines.extend(
            [
                "",
                "## Efficiency",
                "",
                "| Method | Parents | Retrieval vectors | Chunking s | Index s | Query ms mean/p50/p95 |",
                "|---|---:|---:|---:|---:|---:|",
            ]
        )
        for method, row in sorted(analysis["efficiency"].items()):
            lines.append(
                f'| {method} | {row["parent_chunk_count"]} | {row["retrieval_vector_count"]} | '
                f'{row["chunking_seconds"]:.3f} | {row["index_seconds"]:.3f} | '
                f'{row["query_latency_ms_mean"]:.3f}/{row["query_latency_ms_p50"]:.3f}/'
                f'{row["query_latency_ms_p95"]:.3f} |'
            )
    lines.extend(
        [
            "",
            "The prespecified confirmatory p-value, secondary Holm-adjusted p-values, "
            "project-level tables, question-type tables, and boundary strata are stored "
            "in `formal_analysis.json`.",
        ]
    )
    (output_dir / "formal_analysis.md").write_text("\n".join(lines) + "\n", encoding="utf-8")


def main() -> None:
    parser = argparse.ArgumentParser(description="Aggregate strict SEAC runs and compute paper statistics.")
    parser.add_argument("--run-dirs", nargs="+", required=True)
    parser.add_argument("--primary", default="proposed_v6")
    parser.add_argument("--bootstrap-samples", type=int, default=10000)
    parser.add_argument("--seed", type=int, default=20260804)
    parser.add_argument("--output-dir", default="experiments/formal_analysis")
    parser.add_argument("--manifest", default="configs/formal_run_manifest.json")
    parser.add_argument("--frozen-config", default="configs/industrial_seac_frozen.json")
    parser.add_argument(
        "--scope",
        choices=["development", "validation", "locked_test", "all"],
        default="locked_test",
    )
    parser.add_argument("--allow-exploratory", action="store_true")
    args = parser.parse_args()

    root = Path(__file__).resolve().parents[1]
    manifest = json.loads(resolve_input(root, args.manifest).read_text(encoding="utf-8"))
    frozen_path = resolve_input(root, args.frozen_config)
    frozen = json.loads(frozen_path.read_text(encoding="utf-8"))
    manifest_methods = list(manifest["methods"])
    project_splits = {item["project_id"]: item["split"] for item in manifest["projects"]}
    retrieval_rows = []
    boundary_rows = []
    efficiency_rows = []
    validation = []
    seen = set()
    seen_projects = set()
    for value in args.run_dirs:
        run_dir = resolve_input(root, value)
        config, retrieval, boundaries, efficiency, issues = load_run(
            run_dir, root, args.allow_exploratory, frozen, frozen_path
        )
        if list(config["methods"]) != manifest_methods:
            raise ValueError(
                f"{run_dir}: method matrix differs from the frozen manifest"
            )
        run_projects = {row["project_id"] for row in retrieval}
        if len(run_projects) != 1:
            raise ValueError(f"{run_dir}: expected exactly one project, got {sorted(run_projects)}")
        project_id = next(iter(run_projects))
        if project_id not in project_splits:
            raise ValueError(f"{run_dir}: unknown project_id={project_id}")
        project_methods = {(row["project_id"], row["method"]) for row in retrieval}
        duplicates = seen.intersection(project_methods)
        if duplicates:
            raise ValueError(f"Duplicate project/method run inputs: {sorted(duplicates)}")
        seen.update(project_methods)
        seen_projects.add(project_id)
        retrieval_rows.extend(retrieval)
        boundary_rows.extend(boundaries)
        efficiency_rows.extend(efficiency)
        validation.append(
            {
                "run_dir": str(run_dir),
                "project_id": project_id,
                "split": project_splits[project_id],
                "methods": config["methods"],
                "issues": issues,
                "valid_formal_run": not issues,
            }
        )
    expected_projects = set(project_splits)
    if seen_projects != expected_projects and not args.allow_exploratory:
        missing = sorted(expected_projects - seen_projects)
        extra = sorted(seen_projects - expected_projects)
        raise ValueError(f"Formal analysis requires the complete manifest; missing={missing}, extra={extra}")
    scoped_projects = (
        expected_projects
        if args.scope == "all"
        else {project for project, split in project_splits.items() if split == args.scope}
    )
    scoped_retrieval = [row for row in retrieval_rows if row["project_id"] in scoped_projects]
    scoped_boundary = [row for row in boundary_rows if row["project_id"] in scoped_projects]
    scoped_efficiency = [row for row in efficiency_rows if row["project_id"] in scoped_projects]
    if not scoped_retrieval:
        raise ValueError(f"No retrieval records are available for scope={args.scope}")
    confirmatory_comparator = frozen.get("statistical_inference", {}).get("primary_comparator")
    if not confirmatory_comparator:
        raise ValueError("Frozen protocol lacks statistical_inference.primary_comparator")
    analysis = analyze(
        scoped_retrieval,
        scoped_boundary,
        args.primary,
        confirmatory_comparator,
        args.bootstrap_samples,
        args.seed,
    )
    analysis.update(
        {
            "analysis_scope": args.scope,
            "project_ids": sorted(scoped_projects),
            "project_count": len(scoped_projects),
            "efficiency": aggregate_efficiency(scoped_efficiency),
            "project_splits": dict(sorted(project_splits.items())),
        }
    )
    write_outputs(analysis, resolve_input(root, args.output_dir), validation)
    print(
        json.dumps(
            {
                "scope": args.scope,
                "projects": sorted(scoped_projects),
                "methods": sorted(analysis["retrieval"]),
                "retrieval_questions": len(scoped_retrieval),
                "boundary_records": len(scoped_boundary),
            },
            ensure_ascii=False,
            indent=2,
        )
    )


if __name__ == "__main__":
    main()
