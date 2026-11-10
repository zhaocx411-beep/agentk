from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from seac_chunking import evaluate_retrieval, load_corpus
from seac_chunking.models import Chunk
from seac_chunking.semantic import build_encoder


def main():
    parser = argparse.ArgumentParser(description="Evaluate parent/child score fusion without re-encoding for every weight.")
    parser.add_argument("--chunks", required=True)
    parser.add_argument("--corpus", required=True)
    parser.add_argument("--qa", required=True)
    parser.add_argument("--weights", nargs="+", type=float, default=[0.0, 0.15, 0.25, 0.35, 0.5])
    parser.add_argument("--encoder", choices=["tfidf", "bge-m3"], default="bge-m3")
    parser.add_argument("--model-batch-size", type=int, default=8)
    parser.add_argument("--device", default="cpu")
    parser.add_argument("--output", required=True)
    args = parser.parse_args()

    chunks = [Chunk(**item) for item in json.loads(Path(args.chunks).read_text(encoding="utf-8"))]
    questions = json.loads(Path(args.qa).read_text(encoding="utf-8"))
    corpus = load_corpus(args.corpus)
    document_types = {units[0].document: units[0].document_type for units in corpus.values() if units}
    encoder = build_encoder(
        args.encoder,
        batch_size=args.model_batch_size,
        max_length=512,
        device=args.device,
    ) if args.encoder == "bge-m3" else build_encoder(args.encoder)

    results = {}
    for weight in args.weights:
        print(f"[start] parent_weight={weight}", flush=True)
        result = evaluate_retrieval(
            chunks,
            questions,
            encoder=encoder,
            contextualize_all=True,
            document_types=document_types,
            hierarchy_parent_weight=weight,
        )
        results[str(weight)] = {key: value for key, value in result.items() if key != "details"}
        print(f"[done] parent_weight={weight}: {results[str(weight)]}", flush=True)
    Path(args.output).write_text(json.dumps(results, ensure_ascii=False, indent=2), encoding="utf-8")


if __name__ == "__main__":
    main()
