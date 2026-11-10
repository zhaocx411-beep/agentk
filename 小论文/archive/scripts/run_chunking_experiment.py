from __future__ import annotations

import argparse
import csv
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from seac_chunking import build_chunker, evaluate_boundaries, evaluate_retrieval, load_corpus
from seac_chunking.semantic import build_encoder


METHODS = ("fixed", "recursive", "structure", "semantic", "proposed", "proposed_v6")


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
        summary["retrieval_chunk_count"] = len(children)
    return summary


def main():
    parser = argparse.ArgumentParser(description="Run comparable chunking baselines on the extracted project corpus.")
    parser.add_argument("--corpus", default="workbench/P01/corpus.json")
    parser.add_argument("--qa", default="workbench/P01/qa_review_set.json")
    parser.add_argument("--breakpoints", default="workbench/P01/candidate_breakpoints.json")
    parser.add_argument("--methods", nargs="+", default=list(METHODS), choices=METHODS)
    parser.add_argument("--max-tokens", type=int, default=512)
    parser.add_argument("--overlap", type=int, default=64)
    parser.add_argument("--semantic-percentile", type=float, default=0.8)
    parser.add_argument("--proposed-soft-target", type=int, default=320)
    parser.add_argument("--proposed-dense-target", type=int, default=224)
    parser.add_argument("--proposed-min-chunk", type=int, default=48)
    parser.add_argument("--v6-child-target", type=int, default=112)
    parser.add_argument("--v6-child-max", type=int, default=176)
    parser.add_argument("--hierarchy-parent-weight", type=float, default=0.35)
    parser.add_argument("--fair-context", action=argparse.BooleanOptionalAction, default=True)
    parser.add_argument("--encoder", choices=["tfidf", "bge-m3"], default="tfidf")
    parser.add_argument("--model-batch-size", type=int, default=8)
    parser.add_argument("--device", default="cpu")
    parser.add_argument("--output-dir", default="experiments/P01")
    args = parser.parse_args()

    corpus = load_corpus(args.corpus)
    questions = json.loads(Path(args.qa).read_text(encoding="utf-8"))
    candidates = json.loads(Path(args.breakpoints).read_text(encoding="utf-8"))
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
        kwargs = {"max_tokens": args.max_tokens, "overlap": args.overlap}
        if method in {"semantic", "proposed", "proposed_v6"}:
            kwargs["breakpoint_percentile"] = args.semantic_percentile
            kwargs["encoder"] = shared_encoder
        if method in {"proposed", "proposed_v6"}:
            kwargs["soft_target_tokens"] = args.proposed_soft_target
            kwargs["dense_target_tokens"] = args.proposed_dense_target
            kwargs["min_chunk_tokens"] = args.proposed_min_chunk
        if method == "proposed_v6":
            kwargs["child_target_tokens"] = args.v6_child_target
            kwargs["child_max_tokens"] = args.v6_child_max
        chunker = build_chunker(method, **kwargs)
        chunks = chunker.chunk_corpus(corpus)
        document_types = {
            units[0].document: units[0].document_type
            for units in corpus.values()
            if units
        }
        retrieval = evaluate_retrieval(
            chunks,
            questions,
            encoder=shared_encoder,
            contextualize_all=args.fair_context,
            document_types=document_types,
            hierarchy_parent_weight=args.hierarchy_parent_weight,
        )
        boundary = evaluate_boundaries(chunks, candidates, corpus)
        results[method] = {**summarize(chunks), "boundary": boundary, "retrieval": {key: value for key, value in retrieval.items() if key != "details"}}
        (output_dir / f"{method}_chunks.json").write_text(
            json.dumps([chunk.to_dict() for chunk in chunks], ensure_ascii=False, indent=2),
            encoding="utf-8",
        )
        (output_dir / f"{method}_retrieval_details.json").write_text(
            json.dumps(retrieval["details"], ensure_ascii=False, indent=2),
            encoding="utf-8",
        )
        print(f"[done] {method}: chunks={len(chunks)}", flush=True)

    (output_dir / "results.json").write_text(json.dumps(results, ensure_ascii=False, indent=2), encoding="utf-8")
    with (output_dir / "results.csv").open("w", encoding="utf-8-sig", newline="") as handle:
        writer = csv.writer(handle)
        writer.writerow(
            ["method", "chunk_count", "mean_chunk_tokens", "max_chunk_tokens", "boundary_f1", "mrr", "hit_rate@1", "hit_rate@3", "hit_rate@5"]
        )
        for method, result in results.items():
            writer.writerow(
                [
                    method,
                    result["chunk_count"],
                    result["mean_chunk_tokens"],
                    result["max_chunk_tokens"],
                    result["boundary"]["f1"],
                    result["retrieval"]["mrr"],
                    result["retrieval"]["hit_rate@1"],
                    result["retrieval"]["hit_rate@3"],
                    result["retrieval"]["hit_rate@5"],
                ]
            )
    (output_dir / "config.json").write_text(
        json.dumps(
            {
                "corpus": args.corpus,
                "qa": args.qa,
                "breakpoints": args.breakpoints,
                "methods": args.methods,
                "max_tokens": args.max_tokens,
                "overlap": args.overlap,
                "semantic_percentile": args.semantic_percentile,
                "proposed_soft_target": args.proposed_soft_target,
                "proposed_dense_target": args.proposed_dense_target,
                "proposed_min_chunk": args.proposed_min_chunk,
                "v6_child_target": args.v6_child_target,
                "v6_child_max": args.v6_child_max,
                "fair_context": args.fair_context,
                "hierarchy_parent_weight": args.hierarchy_parent_weight,
                "semantic_encoder": args.encoder,
                "model_batch_size": args.model_batch_size,
                "device": args.device,
            },
            ensure_ascii=False,
            indent=2,
        ),
        encoding="utf-8",
    )
    print(json.dumps(results, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
