from __future__ import annotations

import argparse
import csv
import hashlib
import importlib.metadata
import json
import platform
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from seac_chunking import build_chunker, evaluate_boundaries, evaluate_retrieval, load_corpus
from seac_chunking.semantic import build_encoder


CORE_METHODS = ("fixed", "recursive", "structure", "semantic", "proposed", "proposed_v6")
EXTERNAL_METHODS = ("langchain_recursive", "llamaindex_sentence")
ABLATION_METHODS = (
    "seac_no_semantic",
    "seac_no_density",
    "seac_no_continuity",
    "seac_no_adaptive_threshold",
    "seac_no_heading_context",
    "seac_no_table_children",
    "seac_no_fusion",
)
METHODS = CORE_METHODS + EXTERNAL_METHODS + ABLATION_METHODS

IMPLEMENTATION_FILES = (
    "scripts/run_chunking_experiment.py",
    "seac_chunking/chunkers.py",
    "seac_chunking/evaluation.py",
    "seac_chunking/models.py",
    "seac_chunking/semantic.py",
    "seac_chunking/text.py",
    "seac_chunking/units.py",
)


APPROVED_QA_STATUSES = {"KEEP", "APPROVED", "VERIFIED", "已复核", "已确认", "已仲裁"}
PRIVACY_CLEARED_STATUSES = {"CLEARED", "NO_RISK", "ANONYMIZED"}


def sha256(path):
    digest = hashlib.sha256()
    with Path(path).open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def package_version(name):
    try:
        return importlib.metadata.version(name)
    except importlib.metadata.PackageNotFoundError:
        return "not-installed"


def validate_questions(questions, allow_unreviewed=False):
    required = {"qa_id", "question", "reference_answer"}
    invalid = []
    unreviewed = []
    privacy_pending = []
    for index, item in enumerate(questions, 1):
        missing = sorted(required - set(item))
        if missing or not (item.get("gold_evidence_id") or item.get("gold_evidence_unit_id") or item.get("gold_evidence_ids") or item.get("gold_evidence_unit_ids")):
            invalid.append(f'{item.get("qa_id", index)}: missing {missing or ["gold evidence id"]}')
        statuses = {
            str(item.get("audit_status", "")).strip().upper(),
            str(item.get("review_status", "")).strip().upper(),
        }
        if not statuses.intersection(APPROVED_QA_STATUSES):
            unreviewed.append(item.get("qa_id", str(index)))
        if not allow_unreviewed:
            if not item.get("gold_evidence_unit_id") and not item.get("gold_evidence_unit_ids"):
                invalid.append(f'{item.get("qa_id", index)}: missing exact gold evidence unit')
            if not item.get("gold_evidence_span") and not item.get("gold_evidence_spans"):
                invalid.append(f'{item.get("qa_id", index)}: missing exact gold evidence span')
            privacy_status = str(item.get("privacy_review_status", "")).strip().upper()
            if privacy_status not in PRIVACY_CLEARED_STATUSES:
                privacy_pending.append(item.get("qa_id", str(index)))
    if invalid:
        raise ValueError("Invalid QA records: " + "; ".join(invalid[:10]))
    if unreviewed and not allow_unreviewed:
        preview = ", ".join(unreviewed[:10])
        suffix = " ..." if len(unreviewed) > 10 else ""
        raise ValueError(
            f"{len(unreviewed)} QA records are not approved (examples: {preview}{suffix}). "
            "Pass --allow-unreviewed-qa only for exploratory runs."
        )
    if privacy_pending and not allow_unreviewed:
        preview = ", ".join(privacy_pending[:10])
        suffix = " ..." if len(privacy_pending) > 10 else ""
        raise ValueError(
            f"{len(privacy_pending)} QA records lack privacy clearance (examples: {preview}{suffix}). "
            "Formal runs require privacy_review_status=NO_RISK, ANONYMIZED, or CLEARED."
        )


WITHHELD_INPUT = {"none", ""}


def is_withheld(value: str) -> bool:
    """Report whether an evaluation input was explicitly omitted rather than supplied."""

    return str(value).strip().lower() in WITHHELD_INPUT


def summarize(chunks):
    token_counts = [chunk.token_count for chunk in chunks]
    summary = {
        "chunk_count": len(chunks),
        "mean_chunk_tokens": sum(token_counts) / len(token_counts) if token_counts else 0,
        "min_chunk_tokens": min(token_counts, default=0),
        "max_chunk_tokens": max(token_counts, default=0),
    }
    parents = [chunk for chunk in chunks if chunk.metadata.get("retrieval_level") == "parent"]
    children = [chunk for chunk in chunks if chunk.metadata.get("retrieval_level") == "child"]
    if parents or children:
        summary["parent_chunk_count"] = len(parents)
        summary["child_chunk_count"] = len(children)
        # Hierarchical retrieval embeds and ranks both parents and children.
        summary["retrieval_chunk_count"] = len(chunks)
    return summary


def main():
    parser = argparse.ArgumentParser(description="Run comparable chunking baselines on the extracted project corpus.")
    parser.add_argument("--corpus", default="workbench/P01/corpus.json")
    # A project can be withheld from one axis and still contribute to the other:
    # P08 has no promoted boundary gold standard, P10's approved question set is
    # below the per-project minimum. Passing "none" states that omission
    # explicitly, so the axis is reported as absent instead of as a zero score.
    parser.add_argument("--qa", default="workbench/P01/qa_review_set.json")
    parser.add_argument("--breakpoints", default="workbench/P01/candidate_breakpoints.json")
    # Recorded so the analysis can attribute a run whose questions are withheld,
    # instead of inferring the project from the question records.
    parser.add_argument("--project-id", default="")
    parser.add_argument("--methods", nargs="+", default=list(CORE_METHODS), choices=METHODS)
    parser.add_argument("--max-tokens", type=int, default=512)
    parser.add_argument("--overlap", type=int, default=64)
    parser.add_argument("--semantic-percentile", type=float, default=0.8)
    parser.add_argument("--semantic-weight", type=float, default=0.45)
    parser.add_argument("--structure-weight", type=float, default=0.30)
    parser.add_argument("--density-weight", type=float, default=0.25)
    parser.add_argument("--continuity-penalty", type=float, default=0.35)
    parser.add_argument("--protected-group-penalty", type=float, default=1.0)
    parser.add_argument("--structure-heading-signal", type=float, default=1.0)
    parser.add_argument("--structure-path-change-signal", type=float, default=0.7)
    parser.add_argument("--structure-group-change-signal", type=float, default=0.5)
    parser.add_argument("--density-change-scale", type=float, default=10.0)
    parser.add_argument("--table-ratio-threshold-adjustment", type=float, default=0.08)
    parser.add_argument("--heading-ratio-threshold-adjustment", type=float, default=-0.05)
    parser.add_argument("--key-density-cutoff", type=float, default=0.03)
    parser.add_argument("--proposed-soft-target", type=int, default=320)
    parser.add_argument("--proposed-dense-target", type=int, default=224)
    parser.add_argument("--proposed-min-chunk", type=int, default=48)
    parser.add_argument("--v6-child-target", type=int, default=112)
    parser.add_argument("--v6-child-max", type=int, default=176)
    parser.add_argument("--hierarchy-parent-weight", type=float, default=0.35)
    parser.add_argument("--fair-context", action=argparse.BooleanOptionalAction, default=True)
    parser.add_argument("--evidence-mode", choices=["auto", "block", "unit"], default="auto")
    parser.add_argument("--allow-unreviewed-qa", action="store_true")
    parser.add_argument("--allow-provisional-breakpoints", action="store_true")
    parser.add_argument("--encoder", choices=["tfidf", "bge-m3"], default="tfidf")
    parser.add_argument("--model-batch-size", type=int, default=8)
    parser.add_argument("--device", default="cpu")
    parser.add_argument(
        "--protocol-config",
        help="Frozen protocol JSON. Required for a run to be marked formal.",
    )
    parser.add_argument("--output-dir", default="experiments/P01")
    args = parser.parse_args()

    if args.protocol_config and not Path(args.protocol_config).is_file():
        raise FileNotFoundError(f"Frozen protocol config does not exist: {args.protocol_config}")

    corpus = load_corpus(args.corpus)
    retrieval_enabled = not is_withheld(args.qa)
    boundary_enabled = not is_withheld(args.breakpoints)
    if not retrieval_enabled and not boundary_enabled:
        raise SystemExit("both --qa and --breakpoints are withheld; there is nothing to evaluate")
    questions = json.loads(Path(args.qa).read_text(encoding="utf-8")) if retrieval_enabled else []
    candidates = json.loads(Path(args.breakpoints).read_text(encoding="utf-8")) if boundary_enabled else []
    validate_questions(questions, allow_unreviewed=args.allow_unreviewed_qa)
    output_dir = Path(args.output_dir)
    output_dir.mkdir(parents=True, exist_ok=True)
    results = {}
    shared_encoder = build_encoder(
        args.encoder,
        batch_size=args.model_batch_size,
        max_length=args.max_tokens,
        device=args.device,
    ) if args.encoder == "bge-m3" else build_encoder(args.encoder)
    shared_encoder.fit([unit.text for units in corpus.values() for unit in units])

    for method in args.methods:
        print(f"[start] {method}", flush=True)
        if hasattr(shared_encoder, "clear_cache"):
            shared_encoder.clear_cache()
        kwargs = {"max_tokens": args.max_tokens, "overlap": args.overlap}
        if method in {"semantic", "proposed", "proposed_v6", *ABLATION_METHODS}:
            kwargs["breakpoint_percentile"] = args.semantic_percentile
            kwargs["encoder"] = shared_encoder
        if method in {"proposed", "proposed_v6", *ABLATION_METHODS}:
            kwargs["semantic_weight"] = args.semantic_weight
            kwargs["structure_weight"] = args.structure_weight
            kwargs["density_weight"] = args.density_weight
            kwargs["continuity_penalty"] = args.continuity_penalty
            kwargs["protected_group_penalty"] = args.protected_group_penalty
            kwargs["structure_heading_signal"] = args.structure_heading_signal
            kwargs["structure_path_change_signal"] = args.structure_path_change_signal
            kwargs["structure_group_change_signal"] = args.structure_group_change_signal
            kwargs["density_change_scale"] = args.density_change_scale
            kwargs["table_ratio_threshold_adjustment"] = args.table_ratio_threshold_adjustment
            kwargs["heading_ratio_threshold_adjustment"] = args.heading_ratio_threshold_adjustment
            kwargs["key_density_cutoff"] = args.key_density_cutoff
            kwargs["soft_target_tokens"] = args.proposed_soft_target
            kwargs["dense_target_tokens"] = args.proposed_dense_target
            kwargs["min_chunk_tokens"] = args.proposed_min_chunk
        if method in {"proposed_v6", *ABLATION_METHODS}:
            kwargs["child_target_tokens"] = args.v6_child_target
            kwargs["child_max_tokens"] = args.v6_child_max
        chunker = build_chunker(method, **kwargs)
        chunking_started = time.perf_counter()
        chunks = chunker.chunk_corpus(corpus)
        chunking_seconds = time.perf_counter() - chunking_started
        document_types = {
            units[0].document: units[0].document_type
            for units in corpus.values()
            if units
        }
        method_parent_weight = 0.0 if method == "seac_no_fusion" else args.hierarchy_parent_weight
        method_fair_context = args.fair_context and method != "seac_no_heading_context"
        retrieval = (
            evaluate_retrieval(
                chunks,
                questions,
                encoder=shared_encoder,
                contextualize_all=method_fair_context,
                document_types=document_types,
                hierarchy_parent_weight=method_parent_weight,
                evidence_mode=args.evidence_mode,
            )
            if retrieval_enabled
            else None
        )
        boundary = (
            evaluate_boundaries(
                chunks,
                candidates,
                corpus,
                allow_suggested_labels=args.allow_provisional_breakpoints,
            )
            if boundary_enabled
            else None
        )
        boundary_details = boundary.pop("details") if boundary is not None else None
        results[method] = {
            **summarize(chunks),
            "chunking_seconds": chunking_seconds,
            "hierarchy_parent_weight": method_parent_weight,
            "fair_context_applied": method_fair_context,
            "boundary": boundary,
            "retrieval": (
                {key: value for key, value in retrieval.items() if key != "details"}
                if retrieval is not None
                else None
            ),
        }
        (output_dir / f"{method}_chunks.json").write_text(
            json.dumps([chunk.to_dict() for chunk in chunks], ensure_ascii=False, indent=2),
            encoding="utf-8",
        )
        # A withheld axis writes no detail file at all. An empty file would be
        # indistinguishable from a run that legitimately retrieved nothing.
        if retrieval is not None:
            (output_dir / f"{method}_retrieval_details.json").write_text(
                json.dumps(retrieval["details"], ensure_ascii=False, indent=2),
                encoding="utf-8",
            )
        if boundary_details is not None:
            (output_dir / f"{method}_boundary_details.json").write_text(
                json.dumps(boundary_details, ensure_ascii=False, indent=2),
                encoding="utf-8",
            )
        print(f"[done] {method}: chunks={len(chunks)}", flush=True)

    (output_dir / "results.json").write_text(json.dumps(results, ensure_ascii=False, indent=2), encoding="utf-8")
    with (output_dir / "results.csv").open("w", encoding="utf-8-sig", newline="") as handle:
        writer = csv.writer(handle)
        writer.writerow(
            [
                "method", "chunk_count", "parent_chunk_count", "retrieval_chunk_count",
                "mean_chunk_tokens", "max_chunk_tokens", "chunking_seconds",
                "boundary_precision", "boundary_recall", "boundary_f1", "evaluated_candidates", "excluded_noise",
                "mrr", "hit_rate@1", "hit_rate@3", "hit_rate@5",
                "span_mrr", "span_hit_rate@1", "span_hit_rate@3", "span_hit_rate@5",
                "index_seconds", "query_latency_ms_mean", "query_latency_ms_p50", "query_latency_ms_p95",
            ]
        )
        for method, result in results.items():
            # A withheld axis leaves its columns empty. Writing 0 would enter the
            # analysis as a real score of zero.
            boundary = result["boundary"] or {}
            retrieval = result["retrieval"] or {}
            writer.writerow(
                [
                    method,
                    result["chunk_count"],
                    result.get("parent_chunk_count", 0),
                    result.get("retrieval_chunk_count", result["chunk_count"]),
                    result["mean_chunk_tokens"],
                    result["max_chunk_tokens"],
                    result["chunking_seconds"],
                    boundary.get("precision", ""),
                    boundary.get("recall", ""),
                    boundary.get("f1", ""),
                    boundary.get("evaluated_candidates", ""),
                    boundary.get("excluded_noise", ""),
                    retrieval.get("mrr", ""),
                    retrieval.get("hit_rate@1", ""),
                    retrieval.get("hit_rate@3", ""),
                    retrieval.get("hit_rate@5", ""),
                    retrieval.get("span_mrr", ""),
                    retrieval.get("span_hit_rate@1", ""),
                    retrieval.get("span_hit_rate@3", ""),
                    retrieval.get("span_hit_rate@5", ""),
                    retrieval.get("index_seconds", ""),
                    retrieval.get("query_latency_ms_mean", ""),
                    retrieval.get("query_latency_ms_p50", ""),
                    retrieval.get("query_latency_ms_p95", ""),
                ]
            )
    (output_dir / "config.json").write_text(
        json.dumps(
            {
                "project_id": args.project_id,
                "corpus": args.corpus,
                "corpus_sha256": sha256(args.corpus),
                "qa": args.qa if retrieval_enabled else None,
                "qa_sha256": sha256(args.qa) if retrieval_enabled else None,
                "retrieval_withheld": not retrieval_enabled,
                "breakpoints": args.breakpoints if boundary_enabled else None,
                "breakpoints_sha256": sha256(args.breakpoints) if boundary_enabled else None,
                "boundary_withheld": not boundary_enabled,
                "methods": args.methods,
                "max_tokens": args.max_tokens,
                "overlap": args.overlap,
                "semantic_percentile": args.semantic_percentile,
                "semantic_weight": args.semantic_weight,
                "structure_weight": args.structure_weight,
                "density_weight": args.density_weight,
                "continuity_penalty": args.continuity_penalty,
                "protected_group_penalty": args.protected_group_penalty,
                "structure_heading_signal": args.structure_heading_signal,
                "structure_path_change_signal": args.structure_path_change_signal,
                "structure_group_change_signal": args.structure_group_change_signal,
                "density_change_scale": args.density_change_scale,
                "table_ratio_threshold_adjustment": args.table_ratio_threshold_adjustment,
                "heading_ratio_threshold_adjustment": args.heading_ratio_threshold_adjustment,
                "key_density_cutoff": args.key_density_cutoff,
                "proposed_soft_target": args.proposed_soft_target,
                "proposed_dense_target": args.proposed_dense_target,
                "proposed_min_chunk": args.proposed_min_chunk,
                "v6_child_target": args.v6_child_target,
                "v6_child_max": args.v6_child_max,
                "fair_context": args.fair_context,
                "hierarchy_parent_weight": args.hierarchy_parent_weight,
                "evidence_mode": args.evidence_mode,
                "allow_unreviewed_qa": args.allow_unreviewed_qa,
                "allow_provisional_breakpoints": args.allow_provisional_breakpoints,
                "semantic_encoder": args.encoder,
                "model_batch_size": args.model_batch_size,
                "device": args.device,
                "formal_run": bool(args.protocol_config)
                and not args.allow_unreviewed_qa
                and not args.allow_provisional_breakpoints,
                "protocol_config": args.protocol_config,
                "protocol_config_sha256": sha256(args.protocol_config) if args.protocol_config else None,
                "run_status": "COMPLETE",
                "command": [sys.executable, *sys.argv],
                "python_version": platform.python_version(),
                "platform": platform.platform(),
                "implementation_sha256": {
                    path: sha256(Path(__file__).resolve().parents[1] / path)
                    for path in IMPLEMENTATION_FILES
                },
                "package_versions": {
                    "sentence-transformers": package_version("sentence-transformers"),
                    "langchain-text-splitters": package_version("langchain-text-splitters"),
                    "llama-index-core": package_version("llama-index-core"),
                },
            },
            ensure_ascii=False,
            indent=2,
        ),
        encoding="utf-8",
    )
    print(json.dumps(results, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
