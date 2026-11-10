"""Release-safe helpers for constructing boundary review candidates.

This module deliberately contains no project identifiers, document names, or
hand-authored enterprise QA records.  Both internal data preparation and the
anonymous review scaffold may therefore import it without pulling private
benchmark content into the release package.
"""

from __future__ import annotations


def shorten(text: object, limit: int = 420) -> str:
    normalized = " ".join(str(text).split())
    return normalized if len(normalized) <= limit else normalized[: limit - 1] + "…"


def block_evidence(block: dict) -> str:
    if block.get("rows"):
        return " || ".join(" | ".join(str(cell) for cell in row) for row in block["rows"])
    return str(block.get("text", ""))


def make_candidates(documents: list[dict]) -> list[dict]:
    """Create deterministic draft boundary cases for human review.

    The returned labels and reasons are suggestions only.  They must never be
    treated as gold annotations without the independent human review protocol.
    """

    candidates = []
    counter = 1
    for document in documents:
        blocks = document["blocks"]

        heading_boundaries = [
            index
            for index in range(1, len(blocks))
            if blocks[index]["block_type"] == "heading"
        ]
        if heading_boundaries:
            picks = sorted(
                set(
                    heading_boundaries[i * (len(heading_boundaries) - 1) // 4]
                    for i in range(5)
                )
            )
            for index in picks:
                left, right = blocks[index - 1], blocks[index]
                candidates.append(
                    {
                        "candidate_id": f"BP{counter:03d}",
                        "project_id": document["project_id"],
                        "source_document": document["file_name"],
                        "boundary_type": "heading_transition",
                        "left_unit_id": left["block_id"],
                        "right_unit_id": right["block_id"],
                        "left_section": left.get("heading_path", ""),
                        "right_section": right.get("heading_path", ""),
                        "left_evidence": shorten(block_evidence(left)),
                        "right_evidence": shorten(block_evidence(right)),
                        "suggested_label": "BREAK",
                        "suggested_reason": "The right unit enters a new heading or functional section.",
                        "human_label": "",
                        "review_status": "UNLABELED",
                        "review_notes": "",
                    }
                )
                counter += 1

        same_section = []
        for index in range(1, len(blocks)):
            left, right = blocks[index - 1], blocks[index]
            if (
                left.get("heading_path")
                and left.get("heading_path") == right.get("heading_path")
                and right["block_type"] != "heading"
                and left["block_type"] != "heading"
            ):
                same_section.append(index)
        if same_section:
            picks = sorted(
                set(same_section[i * (len(same_section) - 1) // 3] for i in range(4))
            )
            for index in picks:
                left, right = blocks[index - 1], blocks[index]
                candidates.append(
                    {
                        "candidate_id": f"BP{counter:03d}",
                        "project_id": document["project_id"],
                        "source_document": document["file_name"],
                        "boundary_type": "same_section_continuity",
                        "left_unit_id": left["block_id"],
                        "right_unit_id": right["block_id"],
                        "left_section": left.get("heading_path", ""),
                        "right_section": right.get("heading_path", ""),
                        "left_evidence": shorten(block_evidence(left)),
                        "right_evidence": shorten(block_evidence(right)),
                        "suggested_label": "NO_BREAK",
                        "suggested_reason": "Adjacent units share the same heading path.",
                        "human_label": "",
                        "review_status": "UNLABELED",
                        "review_notes": "",
                    }
                )
                counter += 1

        tables = [block for block in blocks if block.get("rows") and len(block["rows"]) >= 3]
        if tables:
            picks = sorted(set(i * (len(tables) - 1) // 2 for i in range(3)))
            for table_index in picks:
                table = tables[table_index]
                row_index = min(max(1, len(table["rows"]) // 2), len(table["rows"]) - 2)
                candidates.append(
                    {
                        "candidate_id": f"BP{counter:03d}",
                        "project_id": document["project_id"],
                        "source_document": document["file_name"],
                        "boundary_type": "inside_table",
                        "left_unit_id": f'{table["block_id"]}#R{row_index:03d}',
                        "right_unit_id": f'{table["block_id"]}#R{row_index + 1:03d}',
                        "left_section": table.get("heading_path", ""),
                        "right_section": table.get("heading_path", ""),
                        "left_evidence": shorten(" | ".join(str(cell) for cell in table["rows"][row_index - 1])),
                        "right_evidence": shorten(" | ".join(str(cell) for cell in table["rows"][row_index])),
                        "suggested_label": "NO_BREAK",
                        "suggested_reason": "The candidate lies inside one table and may break row relationships.",
                        "human_label": "",
                        "review_status": "UNLABELED",
                        "review_notes": "",
                    }
                )
                counter += 1
    return candidates
