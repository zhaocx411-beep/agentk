"""Merge QA re-adjudication verdicts and verify every proposed rewrite against the corpus.

A proposed rewrite is accepted into the candidate set only if the corpus still
supports it: the proposed span must occur in a real unit, the proposed answer
must be contained in that span, and the resulting evidence set must not exceed
the occurrence ceiling. Rewrites that fail these checks are downgraded instead
of trusted.

Every record stays `PENDING`; the candidate set is input for human approval, not
an approved benchmark.
"""

from __future__ import annotations

import argparse
import json
import sys
from collections import Counter
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
sys.path.insert(0, str(Path(__file__).resolve().parent))

from resolve_qa_evidence_equivalents import corpus_units, own_content

PROTOCOL_VERSION = "industrial-seac-qa-readjudication-v1.0"
VERDICTS = {"APPROVED_MULTI_EVIDENCE", "REWRITE_PROPOSED", "REJECTED"}
DIFFICULTIES = {"Easy", "Medium", "Hard"}


def read_verdicts(path: Path) -> list[dict]:
    records = []
    for number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        stripped = line.strip()
        if not stripped:
            continue
        record = json.loads(stripped)
        for field in ("id", "verdict", "difficulty", "proposed_question", "proposed_answer", "proposed_span", "note"):
            if field not in record:
                raise ValueError(f"{path.name}:{number}: missing field {field!r}")
        if record["verdict"] not in VERDICTS:
            raise ValueError(f"{path.name}:{number}: invalid verdict {record['verdict']!r}")
        if record["difficulty"] not in DIFFICULTIES:
            raise ValueError(f"{path.name}:{number}: invalid difficulty {record['difficulty']!r}")
        if not str(record["note"]).strip():
            raise ValueError(f"{path.name}:{number}: empty note for {record['id']}")
        if record["verdict"] == "REWRITE_PROPOSED" and not str(record["proposed_question"]).strip():
            raise ValueError(f"{path.name}:{number}: rewrite without a proposed question")
        if record["verdict"] != "REWRITE_PROPOSED" and any(
            str(record[field]).strip() for field in ("proposed_question", "proposed_answer", "proposed_span")
        ):
            raise ValueError(f"{path.name}:{number}: non-rewrite verdict carries proposal fields")
        records.append(record)
    return records


# Longest first, so that "字段类型为" is removed before "类型为" can split it.
GLUE_TERMS = tuple(
    sorted(
        (
            "参数类型为",
            "字段类型为",
            "取值范围为",
            "是否必填",
            "记录人为",
            "取值范围",
            "分别为",
            "含义为",
            "类型为",
            "长度为",
            "地点为",
            "类型",
            "含义",
            "和",
            "及",
            "为",
            "的",
        ),
        key=len,
        reverse=True,
    )
)
# A faithful answer may state a table cell's meaning rather than copy it.
SYNONYMS = {"必填": ("是",), "非必填": ("否",), "不必填": ("否",), "可空": ("Y", "是"), "不可空": ("N", "否")}
SEGMENT_SEPARATORS = str.maketrans({character: "\n" for character in "，,；;、。（）()|"})


def strip_glue(text: str) -> str:
    for term in GLUE_TERMS:
        text = text.replace(term, "")
    return text


def answer_grounding(answer: str, normalized_span: str) -> dict:
    """Report whether every substantive part of the answer appears in the span.

    Rewritten answers legitimately paraphrase a table row, so literal containment
    is too strict. Connective words are removed from the answer and from the span
    alike, which keeps the comparison symmetric; each remaining answer segment
    must still be traceable to the span, and that is what rules out invented
    facts.
    """

    if own_content(answer) in normalized_span:
        return {"mode": "LITERAL", "ungrounded_segments": []}

    stripped_span = strip_glue(normalized_span)
    ungrounded = []
    for raw in own_content(answer).translate(SEGMENT_SEPARATORS).split("\n"):
        segment = strip_glue(raw)
        if not segment:
            continue
        if segment in stripped_span:
            continue
        if any(strip_glue(alias) in stripped_span for alias in SYNONYMS.get(segment, ())):
            continue
        ungrounded.append(segment)
    return {
        "mode": "PARAPHRASE_GROUNDED" if not ungrounded else "NOT_GROUNDED",
        "ungrounded_segments": ungrounded,
    }


def not_distinctive(record: dict) -> bool:
    """Report whether the record's span recurs beyond the occurrence ceiling.

    A content pre-review can judge a table row unique inside its own table without
    seeing the identical row in the other sections of the same document. Such a row
    is boilerplate, no retrieval system can be graded on picking the right copy, so
    the ceiling overrides the approval rather than the other way round.
    """

    return record.get("evidence_equivalence", {}).get("status") == "TOO_MANY_OCCURRENCES"


def verify_rewrite(record: dict, verdict: dict, units: list[dict], max_occurrences: int) -> dict:
    """Check a proposal against the corpus and return the resulting evidence state."""

    span = verdict["proposed_span"].strip() or record.get("gold_evidence_span", "")
    answer = verdict["proposed_answer"].strip() or record.get("reference_answer", "")
    normalized_span = own_content(span)
    matches = [unit for unit in units if normalized_span and normalized_span in unit["normalized"]]
    failures = []
    if not normalized_span:
        failures.append("empty_span")
    if not matches:
        failures.append("span_not_in_corpus")
    if len(matches) > max_occurrences:
        failures.append("too_many_occurrences")
    grounding = answer_grounding(answer, normalized_span)
    if grounding["mode"] == "NOT_GROUNDED":
        failures.append("answer_not_grounded_in_span")
    return {
        "span": span,
        "answer": answer,
        "answer_grounding": grounding["mode"],
        "ungrounded_segments": grounding["ungrounded_segments"],
        "unit_ids": [unit["unit_id"] for unit in matches],
        "block_ids": sorted({unit["block_id"] for unit in matches}),
        "documents": sorted({unit["document"] for unit in matches}),
        "occurrence_count": len(matches),
        "failures": failures,
    }


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--workbench-root", default="workbench")
    parser.add_argument("--equivalence-root", default="outputs/qa_evidence_equivalence_v1")
    parser.add_argument("--prereview-root", default="outputs/qa_prereview_v1")
    parser.add_argument("--readjudication-root", default="outputs/qa_readjudication_v1")
    parser.add_argument("--manifest", default="configs/formal_run_manifest.json")
    parser.add_argument("--max-occurrences", type=int, default=12)
    args = parser.parse_args()

    root = Path(__file__).resolve().parents[1]
    readjudication_root = root / args.readjudication_root
    manifest = json.loads((readjudication_root / "readjudication_manifest.json").read_text(encoding="utf-8"))
    minimums = {
        project["project_id"]: project.get("minimum_qa_count", 0)
        for project in json.loads((root / args.manifest).read_text(encoding="utf-8"))["projects"]
    }

    summary = {
        "protocol_version": PROTOCOL_VERSION,
        "evidence_model": "multi_evidence_any_hit",
        "max_occurrences": args.max_occurrences,
        "review_source": "LLM_CONTENT_PREREVIEW",
        "formal_gold_standard": False,
        "projects": {},
        "totals": Counter(),
    }
    for project in manifest["projects"]:
        batch = json.loads((readjudication_root / "batches" / f"{project}.json").read_text(encoding="utf-8"))
        verdicts = read_verdicts(readjudication_root / "labels" / f"{project}.jsonl")
        if [item["id"] for item in verdicts] != [item["id"] for item in batch]:
            raise ValueError(f"{project}: verdict ids or order do not match the batch")
        by_id = {item["id"]: item for item in verdicts}

        expanded = json.loads(
            (root / args.equivalence_root / f"qa_evidence_expanded_{project}.json").read_text(encoding="utf-8")
        )
        prereview = {
            item["id"]: item
            for item in json.loads(
                (root / args.prereview_root / f"qa_content_prereview_{project}.json").read_text(encoding="utf-8")
            )
        }
        units = corpus_units(
            json.loads((root / args.workbench_root / project / "corpus.json").read_text(encoding="utf-8"))
        )

        candidates = []
        counts: Counter[str] = Counter()
        rewrite_failures = []
        not_distinctive_ids: list[str] = []
        for record in expanded:
            composite = f'{project}-{record["qa_id"]}'
            verdict = by_id.get(composite)
            earlier = prereview[composite]
            item = {
                key: value
                for key, value in record.items()
                if key not in {"approval_status", "review_status", "difficulty"}
            }
            item.update(
                {
                    "qa_key": composite,
                    "approval_status": "PENDING",
                    "privacy_review_status": "PENDING",
                    "review_source": "LLM_CONTENT_PREREVIEW",
                    "human_approved": False,
                }
            )

            if verdict is None:
                approved = earlier["content_verdict"] == "APPROVED"
                item["candidate_state"] = "APPROVED_SINGLE_ROUND" if approved else "EXCLUDED"
                item["suggested_difficulty"] = earlier["suggested_difficulty"]
                item["prereview_notes"] = earlier["prereview_notes"]
            elif verdict["verdict"] == "REJECTED":
                item["candidate_state"] = "EXCLUDED"
                item["suggested_difficulty"] = verdict["difficulty"]
                item["prereview_notes"] = verdict["note"]
            elif verdict["verdict"] == "APPROVED_MULTI_EVIDENCE":
                item["candidate_state"] = "CANDIDATE_MULTI_EVIDENCE"
                item["suggested_difficulty"] = verdict["difficulty"]
                item["prereview_notes"] = verdict["note"]
            else:
                check = verify_rewrite(record, verdict, units, args.max_occurrences)
                item["suggested_difficulty"] = verdict["difficulty"]
                item["prereview_notes"] = verdict["note"]
                item["proposed_rewrite"] = {
                    "question": verdict["proposed_question"],
                    "answer": check["answer"],
                    "span": check["span"],
                    "answer_grounding": check["answer_grounding"],
                    "ungrounded_segments": check["ungrounded_segments"],
                    "verification_failures": check["failures"],
                }
                if check["failures"]:
                    item["candidate_state"] = "REWRITE_FAILED_VERIFICATION"
                    rewrite_failures.append({"id": composite, "failures": check["failures"]})
                else:
                    item["candidate_state"] = "CANDIDATE_REWRITE"
                    item["question"] = verdict["proposed_question"]
                    item["reference_answer"] = check["answer"]
                    item["gold_evidence_span"] = check["span"]
                    item["gold_evidence_spans"] = [check["span"]]
                    item["gold_evidence_unit_ids"] = check["unit_ids"]
                    item["gold_evidence_ids"] = check["block_ids"]
                    item["evidence_equivalence"] = {
                        "status": "EQUIVALENT_SET" if check["occurrence_count"] > 1 else "UNIQUE",
                        "occurrence_count": check["occurrence_count"],
                        "unit_ids": check["unit_ids"],
                        "block_ids": check["block_ids"],
                        "documents": check["documents"],
                        "cross_document": len(check["documents"]) > 1,
                        "declared_unit_included": True,
                    }

            if item["candidate_state"] in {"APPROVED_SINGLE_ROUND", "CANDIDATE_MULTI_EVIDENCE"} and not_distinctive(
                record
            ):
                item["candidate_state"] = "EXCLUDED"
                item["exclusion_reason"] = "SPAN_NOT_DISTINCTIVE"
                not_distinctive_ids.append(composite)

            counts[item["candidate_state"]] += 1
            candidates.append(item)

        (readjudication_root / f"qa_candidates_{project}.json").write_text(
            json.dumps(candidates, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
        )
        usable = (
            counts["APPROVED_SINGLE_ROUND"] + counts["CANDIDATE_MULTI_EVIDENCE"] + counts["CANDIDATE_REWRITE"]
        )
        summary["projects"][project] = {
            "record_count": len(candidates),
            "states": dict(sorted(counts.items())),
            "usable_candidate_count": usable,
            "minimum_qa_count": minimums.get(project, 0),
            "meets_minimum": usable >= minimums.get(project, 0),
            "rewrite_verification_failures": rewrite_failures,
            "excluded_not_distinctive": not_distinctive_ids,
            "multi_evidence_records": sum(
                1
                for item in candidates
                if item["candidate_state"].startswith("CANDIDATE")
                and item.get("evidence_equivalence", {}).get("occurrence_count", 1) > 1
            ),
        }
        summary["totals"].update(counts)
        summary["totals"]["usable"] += usable

    summary["totals"] = dict(sorted(summary["totals"].items()))
    (readjudication_root / "readjudication_summary.json").write_text(
        json.dumps(summary, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )

    lines = [
        "# QA 复议与多证据模型汇总",
        "",
        "所有记录的 `approval_status` 与 `privacy_review_status` 仍为 `PENDING`，需人工终审。",
        "",
        "| 项目 | 单轮通过 | 多证据放行 | 改写候选 | 改写未通过校验 | 排除 | 可用合计 | 最低题数 | 达标 |",
        "|---|---:|---:|---:|---:|---:|---:|---:|---|",
    ]
    for project, info in summary["projects"].items():
        states = info["states"]
        lines.append(
            f'| {project} | {states.get("APPROVED_SINGLE_ROUND", 0)} | {states.get("CANDIDATE_MULTI_EVIDENCE", 0)} | '
            f'{states.get("CANDIDATE_REWRITE", 0)} | {states.get("REWRITE_FAILED_VERIFICATION", 0)} | '
            f'{states.get("EXCLUDED", 0)} | {info["usable_candidate_count"]} | {info["minimum_qa_count"]} | '
            f'{"是" if info["meets_minimum"] else "否"} |'
        )
    (readjudication_root / "readjudication_summary.md").write_text("\n".join(lines) + "\n", encoding="utf-8")
    print(json.dumps({"totals": summary["totals"], "projects": summary["projects"]}, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
