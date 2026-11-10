from __future__ import annotations

import argparse
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts.finance_dataset_utils import (
    dataset_statistics,
    write_conversion_report,
    write_json,
)


DATASET = "docfinqa"


def convert(raw_dir: Path, output_dir: Path) -> None:
    """Convert DocFinQA into the AutoSEAC-Fin canonical schema.

    The detailed raw-field mapping should be filled after the downloaded files
    are inspected. DocFinQA should be converted after the TAT-QA converter is
    validated because evidence alignment in full documents is the main risk.
    """
    output_dir.mkdir(parents=True, exist_ok=True)
    corpus = []
    qa_splits = {"train": [], "validation": [], "test": []}
    stats = dataset_statistics(corpus, qa_splits)
    write_json(output_dir / "corpus.json", corpus)
    for split, items in qa_splits.items():
        write_json(output_dir / f"qa_{split}.json", items)
    write_json(output_dir / "dataset_statistics.json", stats)
    write_conversion_report(
        output_dir / "conversion_report.md",
        dataset=DATASET,
        raw_dir=raw_dir,
        output_dir=output_dir,
        notes=[
            "Scaffold generated before raw-field mapping is implemented.",
            "Inspect DocFinQA raw files and update docs/dataset_field_mapping.md before filling this converter.",
            "Priority mapping targets: full-document blocks, original evidence ids/spans, formula/program fields, and QA split policy.",
        ],
        statistics=stats,
    )


def main() -> int:
    parser = argparse.ArgumentParser(description="Convert DocFinQA into AutoSEAC-Fin canonical schema.")
    parser.add_argument("--raw-dir", default="workbench/raw/docfinqa")
    parser.add_argument("--output-dir", default="workbench/docfinqa")
    args = parser.parse_args()
    convert(Path(args.raw_dir), Path(args.output_dir))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

