from __future__ import annotations

import importlib.util
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from openpyxl import load_workbook


ROOT = Path(__file__).parents[1]


def load(name: str):
    spec = importlib.util.spec_from_file_location(name, ROOT / "scripts" / f"{name}.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


importer = load("import_qa_approval_workbook")


CORPUS = [
    {
        "file_name": "spec.docx",
        "document_type": "detailed_design",
        "blocks": [
            {
                "block_id": "spec:B0001",
                "block_type": "paragraph",
                "heading_path": "概述",
                "text": "本平台响应时间不超过3秒。",
            },
            {
                "block_id": "spec:B0002",
                "block_type": "table",
                "heading_path": "接口设计 > 库存查询",
                "rows": [
                    ["参数名称", "类型", "必须"],
                    ["size", "Long", "是"],
                ],
            },
            {
                "block_id": "spec:B0003",
                "block_type": "table",
                "heading_path": "系统清单",
                "rows": [
                    ["主档", "主档", "主档"],
                    ["起始时间", "startTime", "String"],
                ],
            },
        ],
    },
    {
        "file_name": "agreement.docx",
        "document_type": "technical_agreement",
        "blocks": [
            {
                "block_id": "agreement:B0001",
                "block_type": "paragraph",
                "heading_path": "性能",
                "text": "本平台响应时间不超过3秒。",
            }
        ],
    },
]

MULTI_EVIDENCE = {
    "qa_id": "QA001",
    "qa_key": "P02-QA001",
    "candidate_state": "CANDIDATE_MULTI_EVIDENCE",
    "question_type": "PERFORMANCE",
    "question": "本平台的响应时间要求是什么？",
    "reference_answer": "响应时间不超过3秒",
    "source_document": "spec.docx",
    "source_section": "概述",
    "gold_evidence_span": "本平台响应时间不超过3秒。",
    "gold_evidence_unit_ids": ["spec:B0001", "agreement:B0001"],
    "gold_evidence_ids": ["spec:B0001", "agreement:B0001"],
    "evidence_equivalence": {"status": "EQUIVALENT_SET", "occurrence_count": 2},
    "suggested_difficulty": "Easy",
    "prereview_notes": "原文在两份文档中重复",
    "suggested_privacy_flags": [],
}
UNIQUE_ROW = {
    "qa_id": "QA002",
    "qa_key": "P02-QA002",
    "candidate_state": "APPROVED_SINGLE_ROUND",
    "question_type": "API",
    "question": "库存查询接口的 size 参数类型是什么？",
    "reference_answer": "Long",
    "source_document": "spec.docx",
    "source_section": "接口设计 > 库存查询",
    "gold_evidence_span": "size | Long | 是",
    "gold_evidence_unit_ids": ["spec:B0002#R002"],
    "gold_evidence_ids": ["spec:B0002"],
    "evidence_equivalence": {"status": "UNIQUE", "occurrence_count": 1},
    "suggested_difficulty": "Easy",
    "prereview_notes": "唯一行",
    "suggested_privacy_flags": ["PROPRIETARY_BUSINESS_DATA"],
}
SECTION_NUMBERED = {
    "qa_id": "QA003",
    "qa_key": "P02-QA003",
    "candidate_state": "APPROVED_SINGLE_ROUND",
    "question_type": "API",
    "question": "2.1.2.1库存查询接口的 size 参数类型是什么？",
    "reference_answer": "Long",
    "source_document": "spec.docx",
    "source_section": "接口设计 > 库存查询",
    "gold_evidence_span": "size | Long | 是",
    "gold_evidence_unit_ids": ["spec:B0002#R002"],
    "gold_evidence_ids": ["spec:B0002"],
    "evidence_equivalence": {"status": "UNIQUE", "occurrence_count": 1},
    "suggested_difficulty": "Easy",
    "prereview_notes": "题干带小节编号",
    "suggested_privacy_flags": [],
}
DATE_PREFIXED = {
    "qa_id": "QA004",
    "qa_key": "P02-QA004",
    "candidate_state": "APPROVED_SINGLE_ROUND",
    "question_type": "PERFORMANCE",
    "question": "2025.7.22召开的会议对响应时间的要求是什么？",
    "reference_answer": "响应时间不超过3秒",
    "source_document": "spec.docx",
    "source_section": "概述",
    "gold_evidence_span": "本平台响应时间不超过3秒。",
    "gold_evidence_unit_ids": ["spec:B0001", "agreement:B0001"],
    "gold_evidence_ids": ["spec:B0001", "agreement:B0001"],
    "evidence_equivalence": {"status": "EQUIVALENT_SET", "occurrence_count": 2},
    "suggested_difficulty": "Easy",
    "prereview_notes": "题干带会议日期",
    "suggested_privacy_flags": [],
}
FLAGGED_SUPPLEMENT = {
    "qa_id": "QS001",
    "qa_key": "P02-QS001",
    "candidate_state": "CANDIDATE_SUPPLEMENT",
    "question_type": "API",
    "question": "起始时间字段的英文名和类型是什么？",
    "reference_answer": "startTime、String",
    "source_document": "spec.docx",
    "source_section": "系统清单",
    "gold_evidence_span": "起始时间 | startTime | String",
    "gold_evidence_unit_ids": ["spec:B0003#R002"],
    "gold_evidence_ids": ["spec:B0003"],
    "evidence_equivalence": {"status": "UNIQUE", "occurrence_count": 1},
    "answer_grounding": "PARAPHRASE_GROUNDED",
    "header_reliability": "UNINFORMATIVE_HEADER",
    "needs_role_check": True,
    "draft_origin": "llm_authored_supplement_for_human_review",
}


class Fixture:
    """A throwaway project tree plus a freshly generated approval workbook."""

    def __init__(self, candidates: list[dict], supplement: list[dict] | None = None) -> None:
        self.root = Path(tempfile.mkdtemp())
        (self.root / "workbench/P02").mkdir(parents=True)
        (self.root / "workbench/P02/corpus.json").write_text(json.dumps(CORPUS, ensure_ascii=False), encoding="utf-8")
        (self.root / "configs").mkdir()
        (self.root / "configs/manifest.json").write_text(
            json.dumps({"projects": [{"project_id": "P02", "minimum_qa_count": 1}]}), encoding="utf-8"
        )
        self.readjudication = self.root / "readjudication"
        self.supplement_root = self.root / "supplement"
        self.approval = self.root / "approval"
        self.readjudication.mkdir()
        self.supplement_root.mkdir()
        (self.readjudication / "qa_candidates_P02.json").write_text(
            json.dumps(candidates, ensure_ascii=False), encoding="utf-8"
        )
        if supplement:
            (self.supplement_root / "qa_supplement_P02.json").write_text(
                json.dumps(supplement, ensure_ascii=False), encoding="utf-8"
            )
        self.workbook_path = self.approval / "SEAC_QA_approval.xlsx"
        self.run_script("build_qa_approval_workbook", ["--output-root", str(self.approval)])

    def run_script(self, name: str, extra: list[str]) -> subprocess.CompletedProcess:
        return subprocess.run(
            [
                sys.executable,
                str(ROOT / "scripts" / f"{name}.py"),
                "--readjudication-root",
                str(self.readjudication),
                "--supplement-root",
                str(self.supplement_root),
                "--manifest",
                str(self.root / "configs/manifest.json"),
                "--projects",
                "P02",
                *extra,
            ],
            check=name == "build_qa_approval_workbook",
            capture_output=True,
            text=True,
        )

    def fill(self, decisions: dict[str, tuple[str, str, str, str]]) -> None:
        """Write reviewer decisions, keyed by `qa_key`, into the four input columns."""

        workbook = load_workbook(self.workbook_path)
        sheet = workbook["P02"]
        for row in sheet.iter_rows(min_row=2):
            values = decisions.get(row[0].value)
            if values is None:
                continue
            for offset, value in enumerate(values):
                row[importer.INPUT_START - 1 + offset].value = value
        workbook.save(self.workbook_path)

    def edit(self, row: int, header: str, value: str) -> None:
        workbook = load_workbook(self.workbook_path)
        workbook["P02"].cell(row=row, column=importer.HEADERS.index(header) + 1, value=value)
        workbook.save(self.workbook_path)

    def run_import(self, write: bool = True) -> dict:
        extra = [
            "--workbench-root",
            str(self.root / "workbench"),
            "--approval-root",
            str(self.approval),
            "--workbook",
            str(self.workbook_path),
        ]
        if write:
            extra.append("--write")
        completed = self.run_script("import_qa_approval_workbook", extra)
        report_path = self.approval / "import_report.json"
        return {
            "returncode": completed.returncode,
            "stderr": completed.stderr,
            "report": json.loads(report_path.read_text(encoding="utf-8")) if report_path.is_file() else {},
        }

    @property
    def target(self) -> Path:
        return self.root / "workbench/P02/qa_approved.json"

    def approved(self) -> list[dict]:
        return json.loads(self.target.read_text(encoding="utf-8"))

    def problems(self, result: dict) -> list[dict]:
        return result["report"]["projects"]["P02"]["problems"]


class ApprovalImportTests(unittest.TestCase):
    """Exercise the only path from a human decision to a formal QA file."""

    def setUp(self) -> None:
        self.fixture = Fixture([MULTI_EVIDENCE, UNIQUE_ROW])

    def clear_all(self) -> None:
        self.fixture.fill(
            {
                "P02-QA001": ("APPROVED", "NO_RISK", "Easy", ""),
                "P02-QA002": ("APPROVED", "NO_RISK", "Easy", ""),
            }
        )

    def test_cleared_rows_become_a_formal_qa_file(self):
        self.fixture.fill(
            {
                "P02-QA001": ("APPROVED", "NO_RISK", "Easy", ""),
                "P02-QA002": ("APPROVED", "ANONYMIZED", "Medium", "厂商名替换为 VENDOR_A"),
            }
        )
        result = self.fixture.run_import()
        self.assertEqual(0, result["returncode"], result["stderr"])
        self.assertTrue(result["report"]["projects"]["P02"]["complete"])
        records = self.fixture.approved()
        self.assertEqual(["QA001", "QA002"], [item["qa_id"] for item in records])
        self.assertTrue(all(item["human_approved"] for item in records))
        self.assertEqual("Medium", records[1]["difficulty"])
        self.assertEqual("multi_evidence_any_hit", records[0]["evidence_model"])

    def test_equivalent_locations_are_resolved_from_the_corpus(self):
        self.clear_all()
        self.fixture.run_import()
        records = self.fixture.approved()
        self.assertEqual(["agreement:B0001", "spec:B0001"], sorted(records[0]["gold_evidence_unit_ids"]))
        self.assertEqual(2, records[0]["evidence_occurrence_count"])
        self.assertEqual(["spec:B0002#R002"], records[1]["gold_evidence_unit_ids"])
        self.assertEqual(1, records[1]["evidence_occurrence_count"])

    def test_rejected_row_is_kept_out_of_the_formal_file(self):
        self.fixture.fill(
            {
                "P02-QA001": ("REJECTED", "NO_RISK", "", "题干可由两处不同事实回答"),
                "P02-QA002": ("APPROVED", "NO_RISK", "Easy", ""),
            }
        )
        result = self.fixture.run_import()
        self.assertEqual(0, result["returncode"], result["stderr"])
        self.assertEqual(["QA002"], [item["qa_id"] for item in self.fixture.approved()])

    def test_privacy_rejection_blocks_an_approved_question(self):
        self.fixture.fill(
            {
                "P02-QA001": ("APPROVED", "REJECTED", "Easy", "含无法脱敏的内网地址"),
                "P02-QA002": ("APPROVED", "NO_RISK", "Easy", ""),
            }
        )
        self.fixture.run_import()
        self.assertEqual(["QA002"], [item["qa_id"] for item in self.fixture.approved()])

    def test_undecided_row_blocks_the_whole_project(self):
        self.fixture.fill({"P02-QA001": ("APPROVED", "NO_RISK", "Easy", "")})
        result = self.fixture.run_import()
        self.assertNotEqual(0, result["returncode"])
        self.assertEqual(["P02-QA002"], result["report"]["projects"]["P02"]["pending"])
        self.assertFalse(self.fixture.target.exists())

    def test_dry_run_never_writes(self):
        self.clear_all()
        result = self.fixture.run_import(write=False)
        self.assertEqual(0, result["returncode"], result["stderr"])
        self.assertFalse(self.fixture.target.exists())

    def test_approval_without_difficulty_is_reported(self):
        self.fixture.fill(
            {
                "P02-QA001": ("APPROVED", "NO_RISK", "", ""),
                "P02-QA002": ("APPROVED", "NO_RISK", "Easy", ""),
            }
        )
        result = self.fixture.run_import()
        problems = self.fixture.problems(result)
        self.assertEqual(1, len(problems))
        self.assertIn("needs a difficulty", problems[0]["problems"][0])
        self.assertFalse(self.fixture.target.exists())

    def test_rejection_without_a_reason_is_reported(self):
        self.fixture.fill(
            {
                "P02-QA001": ("REJECTED", "NO_RISK", "", ""),
                "P02-QA002": ("APPROVED", "NO_RISK", "Easy", ""),
            }
        )
        problems = self.fixture.problems(self.fixture.run_import())
        self.assertIn("rejection without a reason in review_notes", problems[0]["problems"])

    def test_anonymized_row_must_record_the_substitution(self):
        self.fixture.fill(
            {
                "P02-QA001": ("APPROVED", "ANONYMIZED", "Easy", ""),
                "P02-QA002": ("APPROVED", "NO_RISK", "Easy", ""),
            }
        )
        problems = self.fixture.problems(self.fixture.run_import())
        self.assertIn("substitution rule", problems[0]["problems"][0])

    def test_half_decided_row_is_reported(self):
        self.fixture.fill(
            {
                "P02-QA001": ("APPROVED", "", "Easy", ""),
                "P02-QA002": ("APPROVED", "NO_RISK", "Easy", ""),
            }
        )
        problems = self.fixture.problems(self.fixture.run_import())
        self.assertIn("half decided", " ".join(problems[0]["problems"]))

    def test_invalid_status_word_is_reported(self):
        self.fixture.fill(
            {
                "P02-QA001": ("KEEP", "NO_RISK", "Easy", ""),
                "P02-QA002": ("APPROVED", "NO_RISK", "Easy", ""),
            }
        )
        problems = self.fixture.problems(self.fixture.run_import())
        self.assertIn("invalid approval_status 'KEEP'", problems[0]["problems"])

    def test_edited_candidate_column_aborts_the_import(self):
        self.clear_all()
        self.fixture.edit(2, "reference_answer", "响应时间不超过1秒")
        result = self.fixture.run_import()
        self.assertNotEqual(0, result["returncode"])
        self.assertIn("candidate columns were edited: reference_answer", result["stderr"])

    def test_span_missing_from_the_corpus_is_reported(self):
        self.clear_all()
        corpus = json.loads((self.fixture.root / "workbench/P02/corpus.json").read_text(encoding="utf-8"))
        corpus[0]["blocks"][0]["text"] = "本平台响应时间不超过5秒。"
        corpus[1]["blocks"][0]["text"] = "本平台响应时间不超过5秒。"
        (self.fixture.root / "workbench/P02/corpus.json").write_text(
            json.dumps(corpus, ensure_ascii=False), encoding="utf-8"
        )
        problems = self.fixture.problems(self.fixture.run_import())
        self.assertIn("evidence span is absent from the corpus", problems[0]["problems"])

    def test_pervasive_span_is_reported(self):
        self.clear_all()
        corpus = json.loads((self.fixture.root / "workbench/P02/corpus.json").read_text(encoding="utf-8"))
        corpus[0]["blocks"].extend(
            {
                "block_id": f"spec:B01{index:02d}",
                "block_type": "paragraph",
                "heading_path": "重复段",
                "text": "本平台响应时间不超过3秒。",
            }
            for index in range(20)
        )
        (self.fixture.root / "workbench/P02/corpus.json").write_text(
            json.dumps(corpus, ensure_ascii=False), encoding="utf-8"
        )
        problems = self.fixture.problems(self.fixture.run_import())
        self.assertIn("above the ceiling", problems[0]["problems"][0])


class SectionNumberRuleTests(unittest.TestCase):
    """§6.1 is applied mechanically at import, so it can override a human approval."""

    def setUp(self) -> None:
        self.fixture = Fixture([UNIQUE_ROW, SECTION_NUMBERED, DATE_PREFIXED])
        self.fixture.fill(
            {
                "P02-QA002": ("APPROVED", "NO_RISK", "Easy", ""),
                "P02-QA003": ("APPROVED", "NO_RISK", "Easy", ""),
                "P02-QA004": ("APPROVED", "NO_RISK", "Easy", ""),
            }
        )

    def test_the_numbered_question_is_excluded_and_the_date_one_survives(self):
        result = self.fixture.run_import()
        self.assertEqual(0, result["returncode"], result["stderr"])
        project = result["report"]["projects"]["P02"]
        self.assertEqual(
            [
                {
                    "qa_key": "P02-QA003",
                    "rule": "RULE_6_1",
                    "reason": "question_begins_with_section_number",
                    "prefix": "2.1.2.1",
                    "workbook_decision": "APPROVED",
                }
            ],
            project["rule_excluded"],
        )
        self.assertEqual(1, project["counts"]["RULE_EXCLUDED"])
        self.assertEqual(
            ["QA002", "QA004"], sorted(record["qa_id"] for record in self.fixture.approved())
        )

    def test_the_excluded_row_keeps_the_decision_the_reviewer_wrote(self):
        """The rule reports an override; it never rewrites the workbook."""

        self.fixture.run_import()
        workbook = load_workbook(self.fixture.workbook_path)
        decisions = {
            row[0].value: row[importer.INPUT_START - 1].value
            for row in workbook["P02"].iter_rows(min_row=2)
            if row[0].value
        }
        self.assertEqual("APPROVED", decisions["P02-QA003"])

    def test_an_excluded_row_does_not_count_toward_the_minimum(self):
        project = self.fixture.run_import()["report"]["projects"]["P02"]
        self.assertEqual(2, project["approved_count"])


class RejectionReasonTallyTests(unittest.TestCase):
    """The dominant rejection cause is counted from the reason the reviewer wrote."""

    def setUp(self) -> None:
        self.fixture = Fixture([MULTI_EVIDENCE, UNIQUE_ROW, DATE_PREFIXED])

    def tally(self, notes: dict[str, str]) -> int:
        self.fixture.fill(
            {key: ("REJECTED", "NO_RISK", "", note) for key, note in notes.items()}
        )
        result = self.fixture.run_import(write=False)
        return result["report"]["projects"]["P02"]["section_scanning_rejections"]

    def test_a_reason_naming_the_defect_is_counted(self):
        self.assertEqual(
            1,
            self.tally(
                {
                    "P02-QA001": "内容：题干为章节扫描型模板问法（§6.1）。",
                    "P02-QA002": "内容：与 QA001 近重复（§6.6）。",
                    "P02-QA004": "内容：证据不唯一。",
                }
            ),
        )

    def test_a_continuation_inherits_the_preceding_reason(self):
        """Reviewers wrote "同上" rather than repeating the reason on the next row."""

        self.assertEqual(
            2,
            self.tally(
                {
                    "P02-QA001": "内容：题干为章节扫描型模板问法（§6.1）。",
                    "P02-QA002": "内容：同上，「实施范围」纯为文档小节名（§6.1）。",
                    "P02-QA004": "内容：与 QA001 近重复（§6.6）。",
                }
            ),
        )

    def test_a_continuation_after_an_unrelated_reason_is_not_counted(self):
        self.assertEqual(
            0,
            self.tally(
                {
                    "P02-QA001": "内容：与 QA002 近重复（§6.6）。",
                    "P02-QA002": "内容：同上，逐字相同（§6.6）。",
                    "P02-QA004": "内容：证据不唯一。",
                }
            ),
        )

    def test_the_reason_text_is_kept_verbatim(self):
        self.tally({"P02-QA001": "内容：题干为章节扫描型模板问法（§6.1）。"})
        rejected = self.fixture.run_import(write=False)["report"]["projects"]["P02"]["rejected"]
        self.assertEqual("P02-QA001", rejected[0]["qa_key"])
        self.assertEqual("内容：题干为章节扫描型模板问法（§6.1）。", rejected[0]["reason"])


class RoleCheckImportTests(unittest.TestCase):
    """A candidate flagged for a column-role check may not be approved silently."""

    def setUp(self) -> None:
        self.fixture = Fixture([MULTI_EVIDENCE, UNIQUE_ROW], supplement=[FLAGGED_SUPPLEMENT])

    def decide(self, note: str) -> dict:
        self.fixture.fill(
            {
                "P02-QA001": ("APPROVED", "NO_RISK", "Easy", ""),
                "P02-QA002": ("APPROVED", "NO_RISK", "Easy", ""),
                "P02-QS001": ("APPROVED", "NO_RISK", "Medium", note),
            }
        )
        return self.fixture.run_import()

    def test_flagged_row_without_a_note_is_reported(self):
        problems = self.fixture.problems(self.decide(""))
        self.assertEqual("P02-QS001", problems[0]["qa_key"])
        self.assertIn("column-role confirmation", problems[0]["problems"][0])
        self.assertFalse(self.fixture.target.exists())

    def test_flagged_row_with_a_confirmation_is_accepted(self):
        result = self.decide("已核对该表列序，startTime 确为英文字段名列")
        self.assertEqual(0, result["returncode"], result["stderr"])
        self.assertEqual(["QA001", "QA002", "QS001"], [item["qa_id"] for item in self.fixture.approved()])


if __name__ == "__main__":
    unittest.main()
