from __future__ import annotations

import importlib.util
import json
import shutil
import subprocess
import sys
import tempfile
import time
import unittest
import urllib.error
import urllib.request
from pathlib import Path

from openpyxl import Workbook, load_workbook


ROOT = Path(__file__).parents[1]
WORKBOOK = ROOT / "outputs/qa_approval_v1/SEAC_QA_approval.xlsx"
WORKBENCH = ROOT / "workbench"


def load(name: str):
    spec = importlib.util.spec_from_file_location(name, ROOT / "scripts" / f"{name}.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


store_module = load("qa_approval_store")
HEADERS = store_module.HEADERS

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
                "rows": [["参数名称", "类型", "必须"], ["size", "Long", "是"]],
            },
            {
                "block_id": "spec:B0003",
                "block_type": "table",
                "heading_path": "系统清单",
                "rows": [["主档", "主档", "主档"], ["起始时间", "startTime", "String"]],
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

ROWS = [
    {
        "qa_key": "P02-QA001",
        "project_id": "P02",
        "candidate_state": "CANDIDATE_MULTI_EVIDENCE",
        "question_type": "PERFORMANCE",
        "question": "本平台的响应时间要求是什么？",
        "reference_answer": "响应时间不超过3秒",
        "gold_evidence_span": "本平台响应时间不超过3秒。",
        "evidence_occurrences": 2,
        "evidence_documents": "agreement.docx, spec.docx",
        "gold_evidence_unit_ids": "spec:B0001, agreement:B0001",
        "answer_grounding": "",
        "needs_role_check": "",
        "prereview_notes": "原文在两份文档中重复",
        "suggested_privacy_flags": "",
        "suggested_difficulty": "Easy",
    },
    {
        "qa_key": "P02-QA002",
        "project_id": "P02",
        "candidate_state": "APPROVED_SINGLE_ROUND",
        "question_type": "API",
        "question": "库存查询接口的 size 参数类型是什么？",
        "reference_answer": "Long",
        "gold_evidence_span": "size | Long | 是",
        "evidence_occurrences": 1,
        "evidence_documents": "spec.docx",
        "gold_evidence_unit_ids": "spec:B0002#R002",
        "answer_grounding": "",
        "needs_role_check": "",
        "prereview_notes": "唯一行",
        "suggested_privacy_flags": "PROPRIETARY_BUSINESS_DATA",
        "suggested_difficulty": "Easy",
    },
    {
        "qa_key": "P02-QS001",
        "project_id": "P02",
        "candidate_state": "CANDIDATE_SUPPLEMENT",
        "question_type": "API",
        "question": "起始时间字段的英文名和类型是什么？",
        "reference_answer": "startTime、String",
        "gold_evidence_span": "起始时间 | startTime | String",
        "evidence_occurrences": 1,
        "evidence_documents": "spec.docx",
        "gold_evidence_unit_ids": "spec:B0003#R002",
        "answer_grounding": "PARAPHRASE_GROUNDED",
        "needs_role_check": "YES",
        "prereview_notes": "",
        "suggested_privacy_flags": "",
        "suggested_difficulty": "",
    },
]


def synthetic_tree() -> tuple[Path, Path]:
    """Build a throwaway workbook plus the corpus its evidence must resolve against."""

    directory = Path(tempfile.mkdtemp())
    workbench = directory / "workbench"
    (workbench / "P02").mkdir(parents=True)
    (workbench / "P02/corpus.json").write_text(json.dumps(CORPUS, ensure_ascii=False), encoding="utf-8")

    workbook = Workbook()
    workbook.remove(workbook.active)
    workbook.create_sheet("Guide").cell(row=1, column=1, value="SEAC 检索 QA 人工终审工作簿")
    sheet = workbook.create_sheet("P02")
    for column, header in enumerate(HEADERS, 1):
        sheet.cell(row=1, column=column, value=header)
    for index, row in enumerate(ROWS, 2):
        for column, header in enumerate(HEADERS, 1):
            sheet.cell(row=index, column=column, value=row.get(header, ""))
    path = directory / "SEAC_QA_approval.xlsx"
    workbook.save(path)
    return path, workbench


class HighlightTests(unittest.TestCase):
    """The evidence span has to be findable by eye inside a long unit."""

    def test_span_is_marked_inside_prose(self):
        segments = store_module.highlight("前置说明。本平台响应时间不超过3秒。后续说明。", "本平台响应时间不超过3秒。")
        self.assertEqual(["前置说明。", "本平台响应时间不超过3秒。", "后续说明。"], [s["text"] for s in segments])
        self.assertEqual([False, True, False], [s["match"] for s in segments])

    def test_span_matches_across_intervening_whitespace(self):
        segments = store_module.highlight("响应时间\n不超过 3秒", "响应时间不超过3秒")
        self.assertEqual([True], [s["match"] for s in segments])
        self.assertEqual("响应时间\n不超过 3秒", "".join(s["text"] for s in segments))

    def test_every_occurrence_is_marked(self):
        segments = store_module.highlight("abc-abc", "abc")
        self.assertEqual(2, sum(1 for segment in segments if segment["match"]))

    def test_absent_span_leaves_the_text_untouched(self):
        self.assertEqual(
            [{"text": "本平台响应时间不超过3秒。", "match": False}],
            store_module.highlight("本平台响应时间不超过3秒。", "不存在的原文"),
        )

    def test_table_cell_carrying_the_span_is_marked(self):
        evidence = store_module.render_evidence(
            "表头: 参数名称 | 类型 | 必须\n数据行: size | Long | 是", "Long"
        )
        self.assertEqual("table", evidence["kind"])
        self.assertEqual([False, True, False], [column["match"] for column in evidence["columns"]])

    def test_merged_header_cells_are_collapsed_and_keep_the_match(self):
        evidence = store_module.render_evidence(
            "表头: 主档 | 主档 | 主档\n数据行: 起始时间 | startTime | String", "startTime"
        )
        self.assertEqual(3, len(evidence["columns"]))
        self.assertTrue(evidence["columns"][1]["match"])

    def test_paragraph_unit_renders_as_segments(self):
        evidence = store_module.render_evidence("本平台响应时间不超过3秒。", "响应时间")
        self.assertEqual("text", evidence["kind"])
        self.assertTrue(any(segment["match"] for segment in evidence["segments"]))


class StoreTests(unittest.TestCase):
    def setUp(self) -> None:
        self.path, self.workbench = synthetic_tree()
        self.store = store_module.ApprovalStore(self.path, self.workbench)

    def reload(self) -> store_module.ApprovalStore:
        return store_module.ApprovalStore(self.path, self.workbench)

    def test_guide_sheet_is_not_a_project(self):
        self.assertEqual(["P02"], self.store.projects)
        self.assertEqual(3, len(self.store.rows))

    def test_every_corpus_location_of_the_evidence_is_resolved(self):
        payload = self.store.payload(0)
        self.assertEqual(2, payload["occurrences"])
        self.assertEqual(
            ["agreement.docx", "spec.docx"],
            sorted(location["document"] for location in payload["locations"]),
        )
        self.assertTrue(all(location["evidence"]["segments"][0]["match"] for location in payload["locations"]))

    def test_a_table_row_evidence_resolves_to_one_location(self):
        payload = self.store.payload(1)
        self.assertEqual(1, payload["occurrences"])
        self.assertEqual("spec:B0002#R002", payload["locations"][0]["unit_id"])
        self.assertEqual("接口设计 > 库存查询", payload["locations"][0]["heading_path"])

    def test_literal_answers_are_distinguished_from_paraphrases(self):
        self.assertTrue(self.store.payload(0)["answer_is_substring"])
        self.assertTrue(self.store.payload(1)["answer_is_substring"])
        # "startTime、String" joins two cells with a connector absent from the row,
        # so it is a paraphrase the reviewer has to judge rather than verify.
        self.assertFalse(self.store.payload(2)["answer_is_substring"])

    def test_role_check_flag_reaches_the_reviewer(self):
        self.assertTrue(self.store.payload(2)["needs_role_check"])
        self.assertFalse(self.store.payload(1)["needs_role_check"])

    def test_a_vanished_span_is_reported_instead_of_silently_empty(self):
        corpus = json.loads((self.workbench / "P02/corpus.json").read_text(encoding="utf-8"))
        corpus[0]["blocks"][0]["text"] = "本平台响应时间不超过5秒。"
        corpus[1]["blocks"][0]["text"] = "本平台响应时间不超过5秒。"
        (self.workbench / "P02/corpus.json").write_text(json.dumps(corpus, ensure_ascii=False), encoding="utf-8")
        payload = self.reload().payload(0)
        self.assertTrue(payload["span_missing"])
        self.assertEqual(0, payload["occurrences"])

    def test_pervasive_span_is_flagged_against_the_ceiling(self):
        store = store_module.ApprovalStore(self.path, self.workbench, max_occurrences=1)
        self.assertTrue(store.payload(0)["over_ceiling"])
        self.assertFalse(store.payload(1)["over_ceiling"])

    def test_decision_is_written_through_to_the_workbook(self):
        self.store.decide(0, "APPROVED", "NO_RISK", "Easy", "")
        sheet = load_workbook(self.path)["P02"]
        self.assertEqual("APPROVED", sheet.cell(row=2, column=HEADERS.index("approval_status") + 1).value)
        self.assertEqual("Easy", sheet.cell(row=2, column=HEADERS.index("difficulty") + 1).value)
        self.assertEqual("APPROVED", self.reload().rows[0]["approval_status"])

    def test_only_the_four_input_columns_change(self):
        before = [cell.value for cell in load_workbook(self.path)["P02"][2]]
        self.store.decide(0, "REJECTED", "NO_RISK", "", "答案需外部推断")
        after = [cell.value for cell in load_workbook(self.path)["P02"][2]]
        self.assertEqual(before[:15], after[:15])
        self.assertEqual(["REJECTED", "NO_RISK", None, "答案需外部推断"], after[15:19])

    def test_half_decided_row_is_refused(self):
        with self.assertRaisesRegex(ValueError, "必须同时给出"):
            self.store.decide(0, "APPROVED", "", "Easy", "")

    def test_approval_without_a_difficulty_is_refused(self):
        with self.assertRaisesRegex(ValueError, "必须给出难度"):
            self.store.decide(0, "APPROVED", "NO_RISK", "", "")

    def test_rejected_row_may_not_carry_a_difficulty(self):
        with self.assertRaisesRegex(ValueError, "不应带难度"):
            self.store.decide(0, "REJECTED", "NO_RISK", "Easy", "证据不唯一")

    def test_rejection_needs_a_reason(self):
        with self.assertRaisesRegex(ValueError, "写明理由"):
            self.store.decide(0, "REJECTED", "NO_RISK", "", "  ")

    def test_anonymized_row_needs_the_substitution_rule(self):
        with self.assertRaisesRegex(ValueError, "替换规则"):
            self.store.decide(0, "APPROVED", "ANONYMIZED", "Easy", "")

    def test_role_check_row_needs_the_column_confirmation(self):
        with self.assertRaisesRegex(ValueError, "列语义"):
            self.store.decide(2, "APPROVED", "NO_RISK", "Medium", "")
        self.store.decide(2, "APPROVED", "NO_RISK", "Medium", "已核对列序，startTime 为英文字段名列")
        self.assertTrue(self.store.is_decided(self.store.rows[2]))

    def test_unsupported_status_word_is_refused(self):
        with self.assertRaisesRegex(ValueError, "不是 APPROVED"):
            self.store.decide(0, "KEEP", "NO_RISK", "Easy", "")

    def test_a_vanished_workbook_is_not_silently_recreated(self):
        self.path.unlink()
        with self.assertRaisesRegex(ValueError, "no longer exists"):
            self.store.decide(0, "APPROVED", "NO_RISK", "Easy", "")
        self.assertFalse(self.path.exists())

    def test_progress_separates_approvals_from_rejections(self):
        self.store.decide(0, "APPROVED", "NO_RISK", "Easy", "")
        progress = self.store.decide(1, "REJECTED", "NO_RISK", "", "题干不自足")
        self.assertEqual({"total": 3, "decided": 2, "approved": 1}, progress["projects"]["P02"])
        self.assertEqual(1, progress["remaining"])
        self.assertEqual(1, progress["rejected"])

    def test_next_undecided_skips_finished_rows_and_wraps(self):
        self.store.decide(1, "APPROVED", "NO_RISK", "Easy", "")
        self.assertEqual(2, self.store.next_undecided(1))
        self.store.decide(0, "APPROVED", "NO_RISK", "Easy", "")
        self.store.decide(2, "APPROVED", "NO_RISK", "Easy", "已核对列序")
        self.assertIsNone(self.store.next_undecided(0))


class ValidationParityTests(unittest.TestCase):
    """A decision the tool accepts must be one the importer would also accept."""

    def setUp(self) -> None:
        self.path, self.workbench = synthetic_tree()
        self.store = store_module.ApprovalStore(self.path, self.workbench)
        self.importer = load("import_qa_approval_workbook")

    def test_every_accepted_decision_passes_the_importer_check(self):
        accepted = [
            ("APPROVED", "NO_RISK", "Easy", ""),
            ("APPROVED", "ANONYMIZED", "Medium", "厂商名替换为 VENDOR_A"),
            ("APPROVED", "CLEARED", "Hard", ""),
            ("REJECTED", "NO_RISK", "", "答案需外部推断"),
            ("APPROVED", "REJECTED", "Easy", "含无法脱敏的内网地址"),
        ]
        for approval, privacy, difficulty, notes in accepted:
            with self.subTest(approval=approval, privacy=privacy):
                self.store.decide(0, approval, privacy, difficulty, notes)
                decision = {
                    "approval_status": approval,
                    "privacy_review_status": privacy,
                    "difficulty": difficulty,
                    "review_notes": notes,
                    "needs_role_check": False,
                }
                self.assertEqual([], self.importer.validate_decision(decision))


class ServerSmokeTests(unittest.TestCase):
    """The server must serve a candidate and persist a decision end to end."""

    @classmethod
    def setUpClass(cls) -> None:
        cls.path, cls.workbench = synthetic_tree()
        cls.port = 8897
        cls.process = subprocess.Popen(
            [
                sys.executable,
                str(ROOT / "scripts/serve_qa_approval.py"),
                "--workbook",
                str(cls.path),
                "--workbench-root",
                str(cls.workbench),
                "--port",
                str(cls.port),
                "--no-browser",
            ],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
        cls.base = f"http://127.0.0.1:{cls.port}"
        for _ in range(60):
            try:
                urllib.request.urlopen(f"{cls.base}/api/state", timeout=1).read()
                return
            except (urllib.error.URLError, ConnectionError):
                time.sleep(0.1)
        raise RuntimeError("review server did not start")

    @classmethod
    def tearDownClass(cls) -> None:
        cls.process.terminate()
        cls.process.wait(timeout=10)
        shutil.rmtree(cls.path.parent, ignore_errors=True)

    def post(self, route: str, body: dict) -> dict:
        request = urllib.request.Request(
            f"{self.base}{route}",
            data=json.dumps(body).encode("utf-8"),
            headers={"Content-Type": "application/json"},
        )
        return json.loads(urllib.request.urlopen(request, timeout=5).read())

    def test_index_and_assets_are_served(self):
        page = urllib.request.urlopen(f"{self.base}/", timeout=5).read().decode("utf-8")
        self.assertIn("内容结论", page)
        script = urllib.request.urlopen(f"{self.base}/app.js", timeout=5).read().decode("utf-8")
        self.assertIn("NO_RISK", script)

    def test_state_starts_at_an_undecided_candidate(self):
        state = json.loads(urllib.request.urlopen(f"{self.base}/api/state", timeout=5).read())
        self.assertEqual(3, state["progress"]["total"])
        self.assertFalse(state["finished"])
        self.assertEqual("", state["candidate"]["decision"]["approval_status"])

    def test_decision_persists_and_advances(self):
        payload = self.post(
            "/api/decide",
            {"index": 0, "approval": "APPROVED", "privacy": "NO_RISK", "difficulty": "Easy", "notes": ""},
        )
        self.assertEqual(1, payload["progress"]["decided"])
        self.assertEqual(1, payload["candidate"]["index"])
        sheet = load_workbook(self.path)["P02"]
        self.assertEqual("APPROVED", sheet.cell(row=2, column=HEADERS.index("approval_status") + 1).value)

    def test_invalid_decision_is_rejected_with_a_reason(self):
        with self.assertRaises(urllib.error.HTTPError) as caught:
            self.post(
                "/api/decide",
                {"index": 2, "approval": "APPROVED", "privacy": "NO_RISK", "difficulty": "", "notes": ""},
            )
        self.assertEqual(400, caught.exception.code)
        self.assertIn("难度", json.loads(caught.exception.read())["error"])


class RealWorkbookTests(unittest.TestCase):
    """Guard the tool against the shape of the pack actually shipped."""

    def setUp(self) -> None:
        if not WORKBOOK.is_file() or not WORKBENCH.is_dir():
            self.skipTest("approval workbook or workbench is not present")
        self.path = Path(tempfile.mkdtemp()) / WORKBOOK.name
        shutil.copy(WORKBOOK, self.path)

    def test_every_row_of_the_shipped_pack_loads_and_renders(self):
        store = store_module.ApprovalStore(self.path, WORKBENCH)
        self.assertEqual(225, len(store.rows))
        self.assertEqual(9, len(store.projects))
        missing = []
        for index in range(len(store.rows)):
            payload = store.payload(index)
            self.assertTrue(payload["question"])
            if payload["span_missing"]:
                missing.append(payload["qa_key"])
            for location in payload["locations"]:
                evidence = location["evidence"]
                self.assertIn(evidence["kind"], {"text", "table"})
                if evidence["kind"] == "table":
                    self.assertTrue(evidence["columns"])
                else:
                    self.assertTrue(any(segment["match"] for segment in evidence["segments"]))
        # Every shipped candidate passed the equivalence resolver, so a span that
        # no longer resolves would mean the corpus and the pack have drifted apart.
        self.assertEqual([], missing)

    def test_a_decision_on_the_real_pack_survives_a_reload(self):
        """Works whether the shipped pack is still blank or already filled in."""

        store = store_module.ApprovalStore(self.path, WORKBENCH)
        undecided_before = store.progress()["remaining"]
        was_decided = store.is_decided(store.rows[0])
        store.decide(0, "APPROVED", "NO_RISK", "Easy", "冒烟测试")
        reloaded = store_module.ApprovalStore(self.path, WORKBENCH)
        self.assertEqual("APPROVED", reloaded.rows[0]["approval_status"])
        self.assertEqual("冒烟测试", reloaded.rows[0]["review_notes"])
        expected = undecided_before if was_decided else undecided_before - 1
        self.assertEqual(expected, reloaded.progress()["remaining"])


if __name__ == "__main__":
    unittest.main()
