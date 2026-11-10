from __future__ import annotations

import json
from collections import defaultdict
from pathlib import Path

from .models import Unit
from .text import token_count


def _row_text(header: list[str], row: list[str]) -> str:
    header_text = " | ".join(header)
    row_text = " | ".join(row)
    return row_text if row == header else f"表头: {header_text}\n数据行: {row_text}"


def document_units(document: dict) -> list[Unit]:
    units = []
    for block in document["blocks"]:
        rows = block.get("rows")
        if rows:
            header = rows[0]
            for index, row in enumerate(rows, 1):
                text = _row_text(header, row)
                unit_id = f'{block["block_id"]}#R{index:03d}'
                units.append(
                    Unit(
                        unit_id=unit_id,
                        source_block_id=block["block_id"],
                        document=document["file_name"],
                        document_type=document["document_type"],
                        block_type="table_row",
                        heading_path=block.get("heading_path", ""),
                        text=text,
                        token_count=token_count(text),
                        protected_group=block["block_id"],
                        row_index=index,
                    )
                )
        else:
            text = block.get("text", "")
            units.append(
                Unit(
                    unit_id=block["block_id"],
                    source_block_id=block["block_id"],
                    document=document["file_name"],
                    document_type=document["document_type"],
                    block_type=block["block_type"],
                    heading_path=block.get("heading_path", ""),
                    text=text,
                    token_count=token_count(text),
                )
            )
    return units


def load_corpus(path: str | Path) -> dict[str, list[Unit]]:
    documents = json.loads(Path(path).read_text(encoding="utf-8"))
    return {document["file_name"]: document_units(document) for document in documents}


def unit_lookup(corpus: dict[str, list[Unit]]) -> dict[str, Unit]:
    return {unit.unit_id: unit for units in corpus.values() for unit in units}


def block_to_units(corpus: dict[str, list[Unit]]) -> dict[str, list[str]]:
    mapping = defaultdict(list)
    for units in corpus.values():
        for unit in units:
            mapping[unit.source_block_id].append(unit.unit_id)
    return dict(mapping)
