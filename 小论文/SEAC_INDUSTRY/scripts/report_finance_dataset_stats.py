from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts.finance_dataset_utils import DATASET_SPLITS, dataset_statistics, read_json, write_json


def load_qa_splits(dataset_dir: Path) -> dict[str, list[dict]]:
    splits = {}
    for split in DATASET_SPLITS:
        path = dataset_dir / f"qa_{split}.json"
        splits[split] = read_json(path) if path.exists() else []
    return splits


def main() -> int:
    parser = argparse.ArgumentParser(description="Report statistics for an AutoSEAC-Fin canonical dataset.")
    parser.add_argument("--dataset-dir", required=True)
    parser.add_argument("--output")
    args = parser.parse_args()

    dataset_dir = Path(args.dataset_dir)
    corpus_path = dataset_dir / "corpus.json"
    if not corpus_path.exists():
        print(f"Missing corpus file: {corpus_path}", file=sys.stderr)
        return 2

    corpus = read_json(corpus_path)
    qa_splits = load_qa_splits(dataset_dir)
    stats = dataset_statistics(corpus, qa_splits)

    if args.output:
        write_json(args.output, stats)
    else:
        print(json.dumps(stats, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

