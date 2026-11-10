from __future__ import annotations

import argparse
import hashlib
import json
import re
from collections import Counter
from pathlib import Path


PRIMARY = "proposed_v6"
PRIMARY_COMPARATOR = "llamaindex_sentence"
PARENT_ONLY = "proposed"
METHOD_PREFIXES = {
    "FIXED": "fixed",
    "RECURSIVE": "recursive",
    "STRUCTURE": "structure",
    "SEMANTIC": "semantic",
    "LANGCHAIN": "langchain_recursive",
    "LLAMAINDEX": "llamaindex_sentence",
    "PARENT": PARENT_ONLY,
    "SEAC": PRIMARY,
}
METHOD_NAMES = {
    "fixed": "Fixed",
    "recursive": "Recursive",
    "structure": "Structure-only",
    "semantic": "Semantic",
    "langchain_recursive": "LangChain recursive",
    "llamaindex_sentence": "LlamaIndex sentence",
    "proposed": "SEAC parents",
    "proposed_v6": "SEAC",
    "seac_no_semantic": "No semantic distance",
    "seac_no_density": "No density signal",
    "seac_no_continuity": "No continuity penalties",
    "seac_no_adaptive_threshold": "Fixed threshold",
    "seac_no_heading_context": "No heading context",
    "seac_no_table_children": "No table-row children",
    "seac_no_fusion": "No parent fusion",
}
NON_SEAC_BASELINES = [
    "fixed",
    "recursive",
    "structure",
    "semantic",
    "langchain_recursive",
    "llamaindex_sentence",
]
ABLATIONS = {
    "ABL_NO_SEM": "seac_no_semantic",
    "ABL_NO_DEN": "seac_no_density",
    "ABL_NO_CONT": "seac_no_continuity",
    "ABL_FIXED_T": "seac_no_adaptive_threshold",
    "ABL_NO_CTX": "seac_no_heading_context",
    "ABL_NO_TABLE": "seac_no_table_children",
    "ABL_NO_FUSION": "seac_no_fusion",
}
COMPONENT_NAMES = {
    "seac_no_semantic": "semantic distance",
    "seac_no_density": "the density signal and density-adaptive sizing",
    "seac_no_continuity": "numeric continuity penalties",
    "seac_no_adaptive_threshold": "document-adaptive threshold adjustments",
    "seac_no_heading_context": "heading context",
    "seac_no_table_children": "table-row children",
    "seac_no_fusion": "parent–child fusion",
}
PLACEHOLDER_PATTERN = re.compile(r"\{\{(FORMAL_[A-Z0-9_]+)\}\}")


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def metric(value: float) -> str:
    return f"{float(value):.4f}"


def interval(values: list[float]) -> str:
    return f"[{float(values[0]):.4f}, {float(values[1]):.4f}]"


def signed(value: float) -> str:
    value = float(value)
    return f"+{value:.4f}" if value >= 0 else f"−{abs(value):.4f}"


def p_value(value: float) -> str:
    value = float(value)
    return "<0.0001" if value < 0.0001 else f"={value:.4f}"


def holm_adjust(p_values: list[float]) -> list[float]:
    indexed = sorted(enumerate(p_values), key=lambda item: item[1])
    adjusted = [1.0] * len(p_values)
    running = 0.0
    total = len(p_values)
    for rank, (index, value) in enumerate(indexed):
        running = max(running, min(1.0, (total - rank) * value))
        adjusted[index] = running
    return adjusted


def comparison_metric(analysis: dict, method: str, name: str) -> dict:
    matches = [
        item
        for item in analysis["paired_retrieval_comparisons"][method]
        if item["metric"] == name
    ]
    if len(matches) != 1:
        raise ValueError(f"Expected one paired comparison for {method}/{name}, got {len(matches)}")
    return matches[0]


def observed_relation(delta: float, subject: str, object_: str, metric_name: str) -> str:
    if delta > 0:
        return f"{subject} was {abs(delta):.4f} higher than {object_} in {metric_name}"
    if delta < 0:
        return f"{subject} was {abs(delta):.4f} lower than {object_} in {metric_name}"
    return f"{subject} and {object_} were tied in {metric_name}"


def validate_analysis(analysis: dict, manifest: dict) -> None:
    locked_items = [item for item in manifest["projects"] if item["split"] == "locked_test"]
    locked = sorted(item["project_id"] for item in locked_items)
    retrieval_project_count = sum(not item.get("qa_withheld") for item in locked_items)
    boundary_project_count = sum(not item.get("boundary_withheld") for item in locked_items)
    if analysis.get("analysis_scope") != "locked_test":
        raise ValueError("Manuscript results must come from analysis_scope=locked_test")
    if sorted(analysis.get("project_ids", [])) != locked:
        raise ValueError(
            f"Locked project set differs from the manifest: {analysis.get('project_ids')} vs {locked}"
        )
    if int(analysis.get("project_count", 0)) != len(locked):
        raise ValueError("Analysis project_count differs from the locked project set")
    if analysis.get("primary_method") != PRIMARY:
        raise ValueError(f"Primary method must be {PRIMARY}")
    if analysis.get("protocol_version") != "industrial-seac-formal-analysis-v1.1":
        raise ValueError("Manuscript results require formal analysis protocol v1.1")
    if int(analysis.get("bootstrap_samples", 0)) < 10000:
        raise ValueError("Formal manuscript requires at least 10,000 bootstrap samples")
    required_methods = set(manifest["methods"])
    for key in ("retrieval", "boundary", "efficiency"):
        missing = sorted(required_methods - set(analysis.get(key, {})))
        if missing:
            raise ValueError(f"Analysis {key} is missing methods: {missing}")
    if analysis.get("confirmatory_comparator") != PRIMARY_COMPARATOR:
        raise ValueError(f"Confirmatory comparator must be {PRIMARY_COMPARATOR}")
    if analysis.get("confirmatory_metric") != "mrr":
        raise ValueError("Confirmatory retrieval metric must be unit MRR")
    expected_comparisons = len(required_methods) - 1
    expected_secondary_tests = expected_comparisons * 4 - 1
    if int(analysis.get("retrieval_holm_family_size", 0)) != expected_secondary_tests:
        raise ValueError("Secondary retrieval Holm family is incomplete")
    if int(analysis.get("boundary_holm_family_size", 0)) != expected_comparisons:
        raise ValueError("Boundary Holm family is incomplete")
    validations = analysis.get("run_validation", [])
    validation_projects = {item.get("project_id") for item in validations}
    expected_projects = {item["project_id"] for item in manifest["projects"]}
    if validation_projects != expected_projects:
        raise ValueError("Run validation does not cover the complete formal manifest")
    invalid = [item for item in validations if not item.get("valid_formal_run") or item.get("issues")]
    if invalid:
        raise ValueError(f"Formal analysis contains invalid runs: {invalid[:3]}")
    retrieval_tests = [
        item
        for comparisons in analysis.get("paired_retrieval_comparisons", {}).values()
        for item in comparisons
    ]
    boundary_tests = list(analysis.get("paired_boundary_comparisons", {}).values())
    if not retrieval_tests or not boundary_tests:
        raise ValueError("Formal analysis is missing paired project-clustered tests")
    expected_comparison_methods = required_methods - {PRIMARY}
    if set(analysis.get("paired_retrieval_comparisons", {})) != expected_comparison_methods:
        raise ValueError("Paired retrieval comparisons do not cover the frozen method matrix")
    if set(analysis.get("paired_boundary_comparisons", {})) != expected_comparison_methods:
        raise ValueError("Paired boundary comparisons do not cover the frozen method matrix")
    expected_retrieval_metrics = {"mrr", "hit_rate@5", "span_mrr", "span_hit_rate@5"}
    for method, comparisons in analysis["paired_retrieval_comparisons"].items():
        if {item.get("metric") for item in comparisons} != expected_retrieval_metrics:
            raise ValueError(f"Retrieval comparison metrics are incomplete for {method}")
    for item, project_count, axis in [
        *((item, retrieval_project_count, "retrieval") for item in retrieval_tests),
        *((item, boundary_project_count, "boundary") for item in boundary_tests),
    ]:
        expected_permutations = 1 << project_count
        if item.get("randomization_unit") != "project_id":
            raise ValueError("Paired tests must randomize at the project level")
        if int(item.get("cluster_count", 0)) != project_count:
            raise ValueError(
                f"Paired {axis} test cluster count differs from its eligible locked project count"
            )
        if item.get("randomization_mode") != "exact_cluster_enumeration":
            raise ValueError("Locked analysis requires exact cluster enumeration")
        if int(item.get("randomization_permutations", 0)) != expected_permutations:
            raise ValueError("Paired test did not enumerate every project-level assignment")
        raw_p = float(item.get("p_value", -1.0))
        if not 0.0 <= raw_p <= 1.0:
            raise ValueError("Paired test p-value is outside [0, 1]")
        if abs(raw_p * expected_permutations - round(raw_p * expected_permutations)) > 1e-9:
            raise ValueError("Exact paired-test p-value is not on the project-enumeration grid")
        if raw_p < 2 / expected_permutations - 1e-12:
            raise ValueError("Two-sided exact paired-test p-value is below the attainable minimum")
    confirmatory = [item for item in retrieval_tests if item.get("confirmatory_test")]
    if len(confirmatory) != 1 or confirmatory[0] is not comparison_metric(
        analysis, PRIMARY_COMPARATOR, "mrr"
    ):
        raise ValueError("Formal analysis must contain exactly the frozen confirmatory test")
    if confirmatory[0].get("p_value_holm_secondary") is not None:
        raise ValueError("The single confirmatory test must not enter the secondary Holm family")
    for item in retrieval_tests:
        if item is confirmatory[0]:
            continue
        adjusted = item.get("p_value_holm_secondary")
        if adjusted is None or not float(item["p_value"]) <= float(adjusted) <= 1.0:
            raise ValueError("Secondary retrieval test lacks a valid Holm-adjusted p-value")
    secondary = [item for item in retrieval_tests if item is not confirmatory[0]]
    expected_secondary_adjusted = holm_adjust([float(item["p_value"]) for item in secondary])
    if any(
        abs(float(item["p_value_holm_secondary"]) - expected) > 1e-12
        for item, expected in zip(secondary, expected_secondary_adjusted)
    ):
        raise ValueError("Secondary retrieval Holm adjustment is inconsistent with raw p-values")
    expected_boundary_adjusted = holm_adjust([float(item["p_value"]) for item in boundary_tests])
    if any(
        abs(float(item.get("p_value_holm", -1.0)) - expected) > 1e-12
        for item, expected in zip(boundary_tests, expected_boundary_adjusted)
    ):
        raise ValueError("Boundary Holm adjustment is inconsistent with raw p-values")
    for method, comparisons in analysis["paired_retrieval_comparisons"].items():
        for item in comparisons:
            name = item["metric"]
            expected_delta = (
                float(analysis["retrieval"][PRIMARY]["overall_micro"][name])
                - float(analysis["retrieval"][method]["overall_micro"][name])
            )
            if abs(float(item["mean_difference"]) - expected_delta) > 1e-12:
                raise ValueError(f"Paired retrieval effect disagrees with the micro summary for {method}/{name}")
    for method, item in analysis["paired_boundary_comparisons"].items():
        expected_delta = (
            float(analysis["boundary"][PRIMARY]["overall_micro"]["f1"])
            - float(analysis["boundary"][method]["overall_micro"]["f1"])
        )
        if abs(float(item["f1_difference"]) - expected_delta) > 1e-12:
            raise ValueError(f"Paired boundary effect disagrees with the micro summary for {method}")


def project_wins(analysis: dict, left: str, right: str, metric_name: str) -> int:
    projects = sorted(
        set(analysis["retrieval"][left]["by_project"])
        & set(analysis["retrieval"][right]["by_project"])
    )
    return sum(
        analysis["retrieval"][left]["by_project"][project][metric_name]
        > analysis["retrieval"][right]["by_project"][project][metric_name]
        for project in projects
    )


def build_replacements(analysis: dict, annotation: dict | None = None) -> dict[str, str]:
    replacements: dict[str, str] = {}
    if annotation is not None:
        replacements.update(annotation_replacements(annotation))
    retrieval = analysis["retrieval"]
    boundary = analysis["boundary"]

    for prefix, method in METHOD_PREFIXES.items():
        bp = boundary[method]["overall_micro"]
        rt = retrieval[method]["overall_micro"]
        replacements.update(
            {
                f"FORMAL_{prefix}_BP_P": metric(bp["precision"]),
                f"FORMAL_{prefix}_BP_R": metric(bp["recall"]),
                f"FORMAL_{prefix}_BP_F1": metric(bp["f1"]),
                f"FORMAL_{prefix}_BP_CI": interval(bp["f1_ci95"]),
                f"FORMAL_{prefix}_MRR": metric(rt["mrr"]),
                f"FORMAL_{prefix}_H1": metric(rt["hit_rate@1"]),
                f"FORMAL_{prefix}_H3": metric(rt["hit_rate@3"]),
                f"FORMAL_{prefix}_H5": metric(rt["hit_rate@5"]),
                f"FORMAL_{prefix}_SPAN_MRR": metric(rt["span_mrr"]),
                f"FORMAL_{prefix}_SPAN_H5": metric(rt["span_hit_rate@5"]),
            }
        )

    seac_rt = retrieval[PRIMARY]["overall_micro"]
    seac_bp = boundary[PRIMARY]["overall_micro"]
    best_retrieval_baseline = max(
        NON_SEAC_BASELINES,
        key=lambda method: retrieval[method]["overall_micro"]["mrr"],
    )
    best_boundary_baseline = max(
        NON_SEAC_BASELINES,
        key=lambda method: boundary[method]["overall_micro"]["f1"],
    )
    baseline_rt = retrieval[best_retrieval_baseline]["overall_micro"]
    baseline_bp = boundary[best_boundary_baseline]["overall_micro"]
    retrieval_delta = seac_rt["mrr"] - baseline_rt["mrr"]
    boundary_delta = seac_bp["f1"] - baseline_bp["f1"]
    confirmatory_rt = retrieval[PRIMARY_COMPARATOR]["overall_micro"]
    confirmatory_delta = seac_rt["mrr"] - confirmatory_rt["mrr"]
    confirmatory_test = comparison_metric(analysis, PRIMARY_COMPARATOR, "mrr")
    confirmatory_p = confirmatory_test["p_value"]
    boundary_test = analysis["paired_boundary_comparisons"][best_boundary_baseline]
    boundary_adjusted_p = boundary_test["p_value_holm"]

    replacements.update(
        {
            "FORMAL_BOUNDARY_F1": metric(seac_bp["f1"]),
            "FORMAL_UNIT_MRR": metric(seac_rt["mrr"]),
            "FORMAL_UNIT_MRR_GAIN": (
                f"an absolute MRR gain of {retrieval_delta:.4f}"
                if retrieval_delta > 0
                else f"an absolute MRR deficit of {abs(retrieval_delta):.4f}"
                if retrieval_delta < 0
                else "no absolute MRR difference"
            ),
            "FORMAL_HIT5": metric(seac_rt["hit_rate@5"]),
        }
    )

    ablation_drops = []
    for prefix, method in ABLATIONS.items():
        ablation_rt = retrieval[method]["overall_micro"]
        ablation_bp = boundary[method]["overall_micro"]
        drop = seac_rt["mrr"] - ablation_rt["mrr"]
        ablation_drops.append((drop, method))
        replacements.update(
            {
                f"FORMAL_{prefix}_BP": metric(ablation_bp["f1"]),
                f"FORMAL_{prefix}_MRR": metric(ablation_rt["mrr"]),
                f"FORMAL_{prefix}_H5": metric(ablation_rt["hit_rate@5"]),
                f"FORMAL_{prefix}_DELTA": signed(ablation_rt["mrr"] - seac_rt["mrr"]),
            }
        )
    ablation_drops.sort(reverse=True)
    positive_drops = [(drop, method) for drop, method in ablation_drops if drop > 0]
    if positive_drops:
        top_components = [COMPONENT_NAMES[method] for _, method in positive_drops[:2]]
        component_phrase = " and ".join(top_components)
        replacements["FORMAL_ABLATION_SUMMARY"] = (
            f"found the largest observed unit-MRR reductions after removing {component_phrase}"
        )
    else:
        replacements["FORMAL_ABLATION_SUMMARY"] = (
            "did not show a unit-MRR reduction for any single removed component"
        )

    boundary_projects = sorted(
        set(boundary[PRIMARY]["by_project"])
        & set(boundary[best_boundary_baseline]["by_project"])
    )
    retrieval_projects = sorted(
        set(retrieval[PRIMARY]["by_project"])
        & set(retrieval[PRIMARY_COMPARATOR]["by_project"])
    )
    boundary_wins = sum(
        boundary[PRIMARY]["by_project"][project]["f1"]
        > boundary[best_boundary_baseline]["by_project"][project]["f1"]
        for project in boundary_projects
    )
    confirmatory_wins = project_wins(analysis, PRIMARY, PRIMARY_COMPARATOR, "mrr")
    replacements["FORMAL_RQ1_NARRATIVE"] = (
        f"Across the {len(boundary_projects)} boundary-eligible locked projects, SEAC obtained boundary F1 "
        f"{metric(seac_bp['f1'])} (95% CI {interval(seac_bp['f1_ci95'])}); the strongest "
        f"non-SEAC boundary baseline was {METHOD_NAMES[best_boundary_baseline]} at "
        f"{metric(baseline_bp['f1'])}. The absolute SEAC-minus-baseline difference was "
        f"{signed(boundary_delta)} (paired 95% CI {interval(boundary_test['ci95'])}, "
        f"Holm-adjusted p{p_value(boundary_adjusted_p)}), and SEAC had the higher project-level "
        f"F1 on {boundary_wins}/{len(boundary_projects)} projects."
    )
    replacements["FORMAL_RQ2_RQ3_NARRATIVE"] = (
        f"SEAC achieved unit MRR {metric(seac_rt['mrr'])} and span MRR "
        f"{metric(seac_rt['span_mrr'])}. The strongest non-SEAC retrieval baseline was "
        f"{METHOD_NAMES[best_retrieval_baseline]} with unit MRR {metric(baseline_rt['mrr'])}, "
        f"an observed SEAC-minus-baseline difference of {signed(retrieval_delta)}. The "
        f"prespecified confirmatory comparison against {METHOD_NAMES[PRIMARY_COMPARATOR]} "
        f"gave a paired mean difference of {signed(confirmatory_test['mean_difference'])} "
        f"(95% CI {interval(confirmatory_test['ci95'])}, exact two-sided "
        f"p{p_value(confirmatory_p)}); SEAC had higher project-level MRR on "
        f"{confirmatory_wins}/{len(retrieval_projects)} retrieval-eligible locked projects. Its Hit@5 was "
        f"{metric(seac_rt['hit_rate@5'])}."
    )
    largest_drop, largest_method = ablation_drops[0]
    largest_test = comparison_metric(analysis, largest_method, "mrr")
    replacements["FORMAL_RQ4_NARRATIVE"] = (
        f"The largest observed unit-MRR change relative to full SEAC occurred for "
        f"{METHOD_NAMES[largest_method]} ({signed(-largest_drop)}). Its paired "
        f"SEAC-minus-ablation difference was {signed(largest_test['mean_difference'])} "
        f"(95% CI {interval(largest_test['ci95'])}, secondary-family Holm-adjusted "
        f"p{p_value(largest_test['p_value_holm_secondary'])}). The complete table is reported "
        "because a component is not treated as causally necessary solely from the largest observed drop."
    )

    efficiency = analysis["efficiency"]
    baseline_eff = efficiency[best_retrieval_baseline]
    seac_eff = efficiency[PRIMARY]
    replacements.update(
        {
            "FORMAL_BEST_BASE_PARENTS": f"{baseline_eff['parent_chunk_count']:,}",
            "FORMAL_BEST_BASE_VECTORS": f"{baseline_eff['retrieval_vector_count']:,}",
            "FORMAL_BEST_BASE_CHUNK_TIME": f"{baseline_eff['chunking_seconds']:.2f} s",
            "FORMAL_BEST_BASE_INDEX_TIME": f"{baseline_eff['index_seconds']:.2f} s",
            "FORMAL_BEST_BASE_QUERY_MS": (
                f"{baseline_eff['query_latency_ms_mean']:.2f} / "
                f"{baseline_eff['query_latency_ms_p50']:.2f} / "
                f"{baseline_eff['query_latency_ms_p95']:.2f} ms"
            ),
            "FORMAL_SEAC_PARENTS": f"{seac_eff['parent_chunk_count']:,}",
            "FORMAL_SEAC_VECTORS": f"{seac_eff['retrieval_vector_count']:,}",
            "FORMAL_SEAC_CHUNK_TIME": f"{seac_eff['chunking_seconds']:.2f} s",
            "FORMAL_SEAC_INDEX_TIME": f"{seac_eff['index_seconds']:.2f} s",
            "FORMAL_SEAC_QUERY_MS": (
                f"{seac_eff['query_latency_ms_mean']:.2f} / "
                f"{seac_eff['query_latency_ms_p50']:.2f} / "
                f"{seac_eff['query_latency_ms_p95']:.2f} ms"
            ),
        }
    )
    vector_ratio = (
        seac_eff["retrieval_vector_count"] / baseline_eff["retrieval_vector_count"]
        if baseline_eff["retrieval_vector_count"]
        else 0.0
    )
    latency_delta = seac_eff["query_latency_ms_mean"] - baseline_eff["query_latency_ms_mean"]
    replacements["FORMAL_RQ5_NARRATIVE"] = (
        f"Relative to the strongest non-SEAC retrieval baseline ({METHOD_NAMES[best_retrieval_baseline]}), "
        f"SEAC used {vector_ratio:.2f}× as many retrieval vectors. Its pooled mean query "
        f"latency differed by {signed(latency_delta)} ms, while total locked-set chunking and "
        f"indexing times were {seac_eff['chunking_seconds']:.2f} s and "
        f"{seac_eff['index_seconds']:.2f} s, respectively."
    )

    significant_retrieval_gain = confirmatory_delta > 0 and confirmatory_p < 0.05
    significant_boundary_gain = boundary_delta > 0 and boundary_adjusted_p < 0.05
    if significant_retrieval_gain:
        conclusion_clause = (
            "that structure-aware hierarchical chunking improved provenance-aware retrieval over the "
            "prespecified LlamaIndex sentence-splitting comparator on this locked industrial sample"
        )
        conclusion_results = (
            f"a positive confirmatory unit-MRR difference of {confirmatory_delta:.4f} for SEAC "
            f"over {METHOD_NAMES[PRIMARY_COMPARATOR]} (exact two-sided p{p_value(confirmatory_p)})"
        )
        implication = (
            "using explicit document structure and parent–child evidence representations when provenance "
            "is important, subject to the reported index and latency costs"
        )
    elif confirmatory_delta > 0:
        conclusion_clause = (
            "an observed retrieval advantage for SEAC over the prespecified LlamaIndex comparator, "
            "but not a statistically significant confirmatory advantage"
        )
        conclusion_results = (
            f"an observed confirmatory unit-MRR difference of {confirmatory_delta:.4f} for SEAC, "
            f"with exact two-sided p{p_value(confirmatory_p)}"
        )
        implication = (
            "further evaluation of structure-aware hierarchy on additional independent projects before "
            "claiming a general retrieval advantage"
        )
    else:
        conclusion_clause = (
            "that SEAC did not establish a retrieval advantage over the prespecified LlamaIndex comparator on this sample"
        )
        conclusion_results = (
            f"no positive confirmatory unit-MRR difference for SEAC relative to {METHOD_NAMES[PRIMARY_COMPARATOR]}"
        )
        implication = (
            "treating the current design as a bounded industrial evaluation rather than evidence of general superiority"
        )
    if significant_boundary_gain and not significant_retrieval_gain:
        conclusion_clause += ", despite a positive multiplicity-adjusted boundary-F1 difference"
    replacements["FORMAL_CONCLUSION_CLAUSE"] = conclusion_clause
    replacements["FORMAL_CONCLUSION_RESULTS"] = conclusion_results
    replacements["FORMAL_CONCLUSION_IMPLICATION"] = implication
    return replacements


def build_highlights(analysis: dict) -> list[str]:
    retrieval = analysis["retrieval"]
    best_baseline = max(
        NON_SEAC_BASELINES,
        key=lambda method: retrieval[method]["overall_micro"]["mrr"],
    )
    seac_mrr = retrieval[PRIMARY]["overall_micro"]["mrr"]
    baseline_mrr = retrieval[best_baseline]["overall_micro"]["mrr"]
    seac_vectors = analysis["efficiency"][PRIMARY]["retrieval_vector_count"]
    baseline_vectors = analysis["efficiency"][best_baseline]["retrieval_vector_count"]
    vector_ratio = seac_vectors / baseline_vectors if baseline_vectors else 0.0
    highlights = [
        "SEAC chunks heterogeneous enterprise software-engineering documents.",
        "Ten industrial projects provide 66,981 provenance-traceable units.",
        f"Locked-test unit MRR: SEAC {seac_mrr:.4f}; best non-SEAC {baseline_mrr:.4f}.",
        f"SEAC uses {vector_ratio:.2f}× the retrieval vectors of the best baseline.",
        "Project-clustered bootstrap and paired tests quantify uncertainty.",
    ]
    invalid = [item for item in highlights if len(item) > 85]
    if invalid:
        raise ValueError(f"JSS highlight exceeds 85 characters: {invalid}")
    return highlights


def p01_privacy_outcome(reviewed_path: Path) -> dict:
    """Count P01's privacy verdicts from its reviewed questions.

    P01 predates the candidate-set path, so its privacy round is recorded in the
    reviewed file rather than in the approval import report. The verdict counts
    are read from that file because it is what the finalization gate consumes.
    """

    records = json.loads(reviewed_path.read_text(encoding="utf-8"))
    statuses = Counter(str(record.get("privacy_review_status", "")).upper() for record in records)
    return {
        "reviewed": len(records),
        "no_risk": statuses.get("NO_RISK", 0),
        "anonymized": statuses.get("ANONYMIZED", 0),
        "cleared": statuses.get("CLEARED", 0),
        "rejected": statuses.get("REJECTED", 0),
        "retained": len(records) - statuses.get("REJECTED", 0),
    }


def validate_boundary_verification(verification: dict, manifest: dict) -> None:
    """Require the verification summary to match the frozen axis exclusions.

    A failed project may be withheld from boundary evaluation without preventing
    retrieval evaluation.  The summary and frozen manifest must name exactly the
    same withheld projects; otherwise a stale summary could either block a valid
    run or silently change the locked evaluation population.
    """

    expected = sorted(
        project["project_id"]
        for project in manifest["projects"]
        if project.get("boundary_withheld")
    )
    reported = sorted(verification.get("withheld_projects", []))
    blocked = sorted(verification.get("blocked_projects", []))
    if reported != expected:
        raise ValueError(
            "boundary verification withheld projects differ from the frozen manifest: "
            f"summary={reported}, manifest={expected}"
        )
    if blocked != reported:
        raise ValueError(
            "boundary verification blockers must match the declared withheld projects: "
            f"blocked={blocked}, withheld={reported}"
        )
    if not verification.get("promotable"):
        raise ValueError(
            "boundary verification is not promotable; complete human verification before injecting results"
        )


def load_annotation_provenance(
    verification_path: Path,
    qa_path: Path,
    approval_path: Path,
    import_path: Path,
    p01_reviewed_path: Path,
    root: Path,
    manifest: dict,
) -> dict:
    """Collect boundary and QA provenance from the annotation pipeline outputs.

    The promoted gold-standard files are the authority for how many boundaries a
    human actually verified, so the counts are recomputed from them rather than
    assumed from the sampling plan.
    """

    verification = json.loads(verification_path.read_text(encoding="utf-8"))
    qa_summary = json.loads(qa_path.read_text(encoding="utf-8"))
    approval = json.loads(approval_path.read_text(encoding="utf-8"))
    imported = json.loads(import_path.read_text(encoding="utf-8"))
    validate_boundary_verification(verification, manifest)

    verified = 0
    prelabelled = 0
    rule_determined = 0
    for project in manifest["projects"]:
        # A project whose boundary verification failed has no promoted gold standard,
        # so it contributes no provenance and is reported as withheld instead.
        if project.get("boundary_withheld"):
            continue
        breakpoints = Path(project["breakpoints"])
        path = breakpoints if breakpoints.is_absolute() else root / breakpoints
        for record in json.loads(path.read_text(encoding="utf-8")):
            source = record.get("label_source", "HUMAN_VERIFIED")
            if source == "LLM_PRELABEL":
                prelabelled += 1
            elif source == "RULE_4_1":
                rule_determined += 1
            else:
                verified += 1

    return {
        "boundary_verification": {
            "kappa_threshold": verification["kappa_threshold"],
            "human_verified_count": verified,
            "llm_prelabel_count": prelabelled,
            "rule_determined_count": rule_determined,
            "withheld_projects": [
                project["project_id"]
                for project in manifest["projects"]
                if project.get("boundary_withheld")
            ],
            "projects": {
                project: {
                    "cohen_kappa": info["cohen_kappa"],
                    "cohen_kappa_before_rule_amendment": info["cohen_kappa_before_rule_amendment"],
                    "raw_agreement": info["raw_agreement"],
                }
                for project, info in verification["projects"].items()
            },
        },
        "qa_prereview": {
            "record_count": qa_summary["totals"]["records"],
            "verdicts": qa_summary["totals"]["verdicts"],
        },
        "qa_candidates": {
            "candidate_total": approval["candidate_total"],
            "project_count": len(approval["projects"]),
            "multi_evidence_records": sum(
                info["multi_evidence_records"] for info in approval["projects"].values()
            ),
            "min_per_project": min(info["candidate_count"] for info in approval["projects"].values()),
        },
        "qa_approval": {
            "approved": imported["totals"].get("APPROVED", 0),
            "rejected": imported["totals"].get("REJECTED", 0),
            "rule_excluded": imported["totals"].get("RULE_EXCLUDED", 0),
            "project_count": len(imported["projects"]),
            "written_total": sum(info["approved_count"] for info in imported["projects"].values()),
            "section_scanning_rejections": sum(
                info["section_scanning_rejections"] for info in imported["projects"].values()
            ),
            "rule_excluded_projects": sorted(
                project for project, info in imported["projects"].items() if info["rule_excluded"]
            ),
            "withheld_projects": [
                project["project_id"] for project in manifest["projects"] if project.get("qa_withheld")
            ],
            "counts": {
                project: info["approved_count"] for project, info in imported["projects"].items()
            },
            "minimums": {
                project: info["minimum_qa_count"] for project, info in imported["projects"].items()
            },
        },
        "p01_privacy": p01_privacy_outcome(p01_reviewed_path),
    }


def withheld_qa_sentence(approval: dict) -> str:
    """State which projects fell below the question minimum, and by how much.

    The sentence has to name the shortfall rather than only the project, because a
    reader checking whether the exclusion was justified needs both numbers.
    """

    withheld = approval["withheld_projects"]
    if not withheld:
        return "No project fell below the minimum."
    parts = [
        f'{project} retained {approval["counts"][project]} of the '
        f'{approval["minimums"][project]} required questions'
        for project in withheld
    ]
    return f'{"; ".join(parts)}.'


def annotation_replacements(annotation: dict) -> dict[str, str]:
    """Build the annotation-provenance placeholders from the verification artifacts.

    These values describe how the boundary reference standard and the question
    drafts were produced. They are read from the annotation pipeline outputs
    rather than from retrieval results, and the manuscript must state them
    whenever pre-labels contributed to the reference standard.
    """

    verification = annotation["boundary_verification"]
    qa = annotation["qa_prereview"]
    verified = verification["human_verified_count"]
    prelabelled = verification["llm_prelabel_count"]
    rule_determined = verification["rule_determined_count"]
    total = verified + prelabelled + rule_determined
    if total <= 0:
        raise ValueError("annotation provenance reports no boundary candidates")

    per_project = ", ".join(
        f'{project} kappa {info["cohen_kappa"]:.2f} after the amendment and '
        f'{info["cohen_kappa_before_rule_amendment"]:.2f} before it, with '
        f'{info["raw_agreement"]:.1%} raw agreement'
        for project, info in sorted(verification["projects"].items())
    )
    # State the direction of the shift explicitly: a reader checking whether the
    # amendment was tuned to clear the threshold needs to see both signs.
    raised = sorted(
        project
        for project, info in verification["projects"].items()
        if info["cohen_kappa"] > info["cohen_kappa_before_rule_amendment"]
    )
    lowered = sorted(
        project
        for project, info in verification["projects"].items()
        if info["cohen_kappa"] < info["cohen_kappa_before_rule_amendment"]
    )
    shift = (
        f'raising it for {", ".join(raised) if raised else "no project"} '
        f'and lowering it for {", ".join(lowered) if lowered else "no project"}'
    )
    withheld = verification["withheld_projects"]
    verdicts = qa["verdicts"]
    candidates = annotation["qa_candidates"]
    approval = annotation["qa_approval"]
    privacy = annotation["p01_privacy"]
    return {
        "FORMAL_PRELABEL_KAPPA_THRESHOLD": f'{verification["kappa_threshold"]:.2f}',
        "FORMAL_PRELABEL_AGREEMENT_SUMMARY": per_project,
        "FORMAL_PRELABEL_KAPPA_SHIFT_SUMMARY": shift,
        "FORMAL_BOUNDARY_WITHHELD_PROJECTS": ", ".join(withheld) if withheld else "no project",
        "FORMAL_HUMAN_VERIFIED_SHARE": f"{verified} of {total} ({verified / total:.1%})",
        "FORMAL_RULE_DETERMINED_SHARE": f"{rule_determined} of {total} ({rule_determined / total:.1%})",
        "FORMAL_LLM_PRELABEL_SHARE": f"{prelabelled} of {total} ({prelabelled / total:.1%})",
        "FORMAL_QA_PREREVIEW_SUMMARY": (
            f'{verdicts.get("APPROVED", 0)} of {qa["record_count"]} drafts were approved as written, '
            f'{verdicts.get("NEEDS_REWRITE", 0)} required a rewrite, and {verdicts.get("REJECTED", 0)} were rejected'
        ),
        "FORMAL_QA_CANDIDATE_SUMMARY": (
            f'{candidates["candidate_total"]} questions across {candidates["project_count"]} projects, '
            f'{candidates["multi_evidence_records"]} of them carrying an evidence equivalence set, '
            f'with a per-project minimum of {candidates["min_per_project"]}'
        ),
        "FORMAL_QA_APPROVAL_SUMMARY": (
            f'{approval["approved"]} of {approval["approved"] + approval["rejected"]} candidates '
            f'across {approval["project_count"]} projects'
        ),
        "FORMAL_QA_REJECTION_SUMMARY": (
            f'{approval["rejected"]} candidates were rejected, of which '
            f'{approval["section_scanning_rejections"]} name a document section rather than '
            f"asking the fact itself"
        ),
        "FORMAL_QA_RULE_EXCLUDED_SUMMARY": (
            f'{approval["rule_excluded"]} approved questions in '
            f'{", ".join(approval["rule_excluded_projects"])}, '
            f'leaving {approval["written_total"]} in the formal question files'
        ),
        "FORMAL_QA_WITHHELD_SUMMARY": withheld_qa_sentence(approval),
        "FORMAL_P01_PRIVACY_SUMMARY": (
            f'{privacy["reviewed"]} P01 questions received an individual privacy verdict: '
            f'{privacy["no_risk"]} carried no identifiable information, '
            f'{privacy["cleared"]} was cleared for controlled use under authorization, and '
            f'{privacy["rejected"]} were rejected for stating a customer or affiliate identity '
            f'in the answer and the evidence span, leaving {privacy["retained"]} questions'
        ),
    }


def fill_manuscript(source: str, replacements: dict[str, str]) -> str:
    placeholders = set(PLACEHOLDER_PATTERN.findall(source))
    missing = sorted(placeholders - set(replacements))
    unused = sorted(set(replacements) - placeholders)
    if missing or unused:
        raise ValueError(f"Placeholder mapping mismatch; missing={missing}, unused={unused}")
    output = PLACEHOLDER_PATTERN.sub(lambda match: replacements[match.group(1)], source)
    if PLACEHOLDER_PATTERN.search(output):
        raise ValueError("Formal placeholders remain after result injection")
    return output


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Fill every formal SEAC manuscript result from a validated locked-test analysis."
    )
    parser.add_argument("--analysis", default="experiments/formal_analysis/formal_analysis.json")
    parser.add_argument("--manifest", default="configs/formal_run_manifest.json")
    parser.add_argument(
        "--verification-summary",
        default="outputs/prelabel_verification_v1/verification_summary.json",
    )
    parser.add_argument("--qa-prereview-summary", default="outputs/qa_prereview_v1/qa_prereview_summary.json")
    parser.add_argument("--qa-approval-manifest", default="outputs/qa_approval_v1/approval_manifest.json")
    parser.add_argument("--qa-import-report", default="outputs/qa_approval_v1/import_report.json")
    parser.add_argument(
        "--p01-qa-reviewed",
        default="workbench/P01/annotation_review/qa_reviewed.json",
    )
    parser.add_argument("--source", default="manuscript/seac_industry_manuscript.md")
    parser.add_argument("--output", default="manuscript/seac_industry_completed.md")
    parser.add_argument("--audit-output", default="manuscript/result_injection_audit.json")
    parser.add_argument("--highlights-output", default="submission/JSS/highlights.txt")
    parser.add_argument("--force", action="store_true")
    args = parser.parse_args()

    root = Path(__file__).resolve().parents[1]
    resolve = lambda value: Path(value) if Path(value).is_absolute() else root / value
    analysis_path = resolve(args.analysis)
    manifest_path = resolve(args.manifest)
    source_path = resolve(args.source)
    output_path = resolve(args.output)
    audit_path = resolve(args.audit_output)
    highlights_path = resolve(args.highlights_output)
    if (output_path.exists() or audit_path.exists() or highlights_path.exists()) and not args.force:
        raise FileExistsError("Completed manuscript, audit, or highlights already exist; archive them or pass --force")

    analysis = json.loads(analysis_path.read_text(encoding="utf-8"))
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    validate_analysis(analysis, manifest)
    annotation = load_annotation_provenance(
        resolve(args.verification_summary),
        resolve(args.qa_prereview_summary),
        resolve(args.qa_approval_manifest),
        resolve(args.qa_import_report),
        resolve(args.p01_qa_reviewed),
        root,
        manifest,
    )
    source = source_path.read_text(encoding="utf-8")
    replacements = build_replacements(analysis, annotation)
    completed = fill_manuscript(source, replacements)
    highlights = build_highlights(analysis)
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(completed, encoding="utf-8")
    highlights_path.parent.mkdir(parents=True, exist_ok=True)
    highlights_path.write_text("\n".join(f"• {item}" for item in highlights) + "\n", encoding="utf-8")
    audit = {
        "protocol_version": "industrial-seac-result-injection-v1.0",
        "analysis_sha256": sha256(analysis_path),
        "manifest_sha256": sha256(manifest_path),
        "source_manuscript_sha256": sha256(source_path),
        "output_manuscript_sha256": sha256(output_path),
        "highlights_sha256": sha256(highlights_path),
        "analysis_scope": analysis["analysis_scope"],
        "project_ids": analysis["project_ids"],
        "replacement_count": len(replacements),
        "replacements": dict(sorted(replacements.items())),
    }
    audit_path.write_text(json.dumps(audit, ensure_ascii=False, indent=2), encoding="utf-8")
    print(
        json.dumps(
            {
                "output": str(output_path),
                "audit": str(audit_path),
                "highlights": str(highlights_path),
                "replacement_count": len(replacements),
            },
            ensure_ascii=False,
            indent=2,
        )
    )


if __name__ == "__main__":
    main()
