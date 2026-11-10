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
WORKBOOK = ROOT / "outputs/annotation_workbooks_v1/SEAC_QA_privacy_review.xlsx"
CORPUS = ROOT / "workbench/P01/corpus.json"
HEADERS = [
    "qa_id",
    "project_id",
    "question",
    "reference_answer",
    "source_document",
    "source_section",
    "question_type",
    "difficulty",
    "gold_evidence_id",
    "gold_evidence_unit_id",
    "gold_evidence_span",
    "evidence_alignment_status",
    "privacy_flags",
    "approval_status",
    "privacy_review_status",
    "review_notes",
    "privacy_review_notes",
]


def load(name: str):
    spec = importlib.util.spec_from_file_location(name, ROOT / "scripts" / f"{name}.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


store_module = load("privacy_review_store")


def synthetic_corpus(path: Path) -> Path:
    """Two documents sharing an engagement prefix, so the identifier is derivable."""

    documents = [
        {
            "file_name": "锦程智慧安全二期-接口文档.docx",
            "document_type": "API",
            "project_id": "P01",
            "blocks": [
                {
                    "block_id": "锦程智慧安全二期-接口文档:B0001",
                    "block_index": 0,
                    "block_type": "paragraph",
                    "heading_path": "概述",
                    "text": "合同四方会签的请求方式为 POST，返回 JSON。",
                },
                {
                    "block_id": "锦程智慧安全二期-接口文档:B0002",
                    "block_index": 1,
                    "block_type": "paragraph",
                    "heading_path": "概述",
                    "text": "本系统由锦程集团安全部负责验收。",
                },
            ],
        },
        {
            # Diverges after the company name, as the real P01 file names do, so
            # the shared prefix is the bare identifier rather than the engagement.
            "file_name": "锦程安全二期技术协议.docx",
            "document_type": "CONTRACT",
            "project_id": "P01",
            "blocks": [
                {
                    "block_id": "锦程智慧安全二期-技术协议:B0001",
                    "block_index": 0,
                    "block_type": "paragraph",
                    "heading_path": "范围",
                    "text": "本协议适用于二期建设。",
                }
            ],
        },
    ]
    path.write_text(json.dumps(documents, ensure_ascii=False), encoding="utf-8")
    return path


def synthetic_workbook(path: Path) -> Path:
    """Three rows: metadata-only hit, content hit, and no hit at all."""

    rows = [
        {
            "qa_id": "QA001",
            "question": "合同四方会签使用什么请求方式？",
            "reference_answer": "POST",
            "source_document": "锦程智慧安全二期-接口文档.docx",
            "source_section": "概述",
            "gold_evidence_span": "请求方式为 POST",
        },
        {
            "qa_id": "QA002",
            "question": "系统由谁负责验收？",
            "reference_answer": "锦程集团安全部",
            "source_document": "锦程智慧安全二期-接口文档.docx",
            "source_section": "概述",
            "gold_evidence_span": "由锦程集团安全部负责验收",
        },
        {
            "qa_id": "QA003",
            "question": "本协议适用于哪一期建设？",
            "reference_answer": "二期",
            "source_document": "技术协议.docx",
            "source_section": "范围",
            "gold_evidence_span": "适用于二期建设",
        },
    ]
    workbook = Workbook()
    workbook.remove(workbook.active)
    sheet = workbook.create_sheet("P01")
    for column, header in enumerate(HEADERS, 1):
        sheet.cell(row=1, column=column, value=header)
    for index, row in enumerate(rows, 2):
        values = {
            "project_id": "P01",
            "question_type": "API",
            "difficulty": "待人工标注",
            "gold_evidence_id": "锦程智慧安全二期-接口文档:B0001",
            "gold_evidence_unit_id": "锦程智慧安全二期-接口文档:B0001",
            "evidence_alignment_status": "BLOCK_IS_UNIT",
            "privacy_flags": "CUSTOMER_OR_VENDOR_IDENTITY",
            "approval_status": "APPROVED",
            "privacy_review_status": "PENDING",
            "review_notes": "证据准确。",
            "privacy_review_notes": None,
            **row,
        }
        if row["qa_id"] == "QA002":
            values["gold_evidence_unit_id"] = "锦程智慧安全二期-接口文档:B0002"
            values["gold_evidence_id"] = "锦程智慧安全二期-接口文档:B0002"
        if row["qa_id"] == "QA003":
            values["gold_evidence_unit_id"] = "锦程智慧安全二期-技术协议:B0001"
            values["gold_evidence_id"] = "锦程智慧安全二期-技术协议:B0001"
        for column, header in enumerate(HEADERS, 1):
            sheet.cell(row=index, column=column, value=values.get(header))
    workbook.save(path)
    return path


class IdentifierDerivationTests(unittest.TestCase):
    """The engagement name must come from the corpus, not from the command line."""

    def test_the_shared_prefix_of_the_file_names_is_the_identifier(self):
        # Names are invented but keep the real shape: the engagement diverges after
        # the company, so the prefix stops at the company itself.
        names = [
            "锦程安全二期技术协议V1.0.docx",
            "锦程智慧安全二期-详细设计规格-接口文档.docx",
            "锦程智慧安全二期项目需求规格书V1.00.docx",
        ]
        self.assertEqual("锦程", store_module.shared_prefix(names))

    def test_an_unrelated_set_of_names_yields_no_identifier(self):
        names = ["接口文档.docx", "技术协议.docx"]
        self.assertEqual("", store_module.shared_prefix(names))

    def test_a_one_character_prefix_is_too_short_to_be_a_name(self):
        self.assertEqual("", store_module.shared_prefix(["A文档.docx", "A协议.docx"]))

    def test_a_trailing_separator_is_not_part_of_the_name(self):
        names = ["锦程-接口.docx", "锦程-协议.docx"]
        self.assertEqual("锦程", store_module.shared_prefix(names))

    def test_file_names_that_never_diverge_yield_a_prefix_too_long_to_match_content(self):
        # Known limitation: when every file name shares the engagement, the derived
        # string is the engagement, which will not match a bare company name in an
        # answer. `--identifier` exists to close this, and the view lists whatever
        # it is matching so a "no hit" verdict can be checked.
        names = ["锦程智慧安全二期-接口.docx", "锦程智慧安全二期-协议.docx"]
        derived = store_module.shared_prefix(names)
        self.assertEqual("锦程智慧安全二期", derived)
        self.assertNotIn(derived, "本系统由锦程集团安全部负责验收。")


class HighlightTests(unittest.TestCase):
    def test_every_occurrence_becomes_a_sensitive_run(self):
        runs = store_module.highlight("锦程集团与锦程安全部", ["锦程"])
        self.assertEqual(
            [
                {"text": "锦程", "sensitive": True},
                {"text": "集团与", "sensitive": False},
                {"text": "锦程", "sensitive": True},
                {"text": "安全部", "sensitive": False},
            ],
            runs,
        )

    def test_plain_text_is_not_split_per_character(self):
        runs = store_module.highlight("请求方式为 POST", ["锦程"])
        self.assertEqual([{"text": "请求方式为 POST", "sensitive": False}], runs)

    def test_the_longest_identifier_wins_over_a_prefix_of_it(self):
        runs = store_module.highlight("锦程集团安全部", ["锦程", "锦程集团"])
        self.assertEqual("锦程集团", runs[0]["text"])
        self.assertTrue(runs[0]["sensitive"])

    def test_no_identifier_leaves_the_text_whole(self):
        self.assertEqual([{"text": "abc", "sensitive": False}], store_module.highlight("abc", []))


class StoreTests(unittest.TestCase):
    def setUp(self) -> None:
        self.directory = Path(tempfile.mkdtemp())
        self.path = synthetic_workbook(self.directory / "privacy.xlsx")
        self.corpus = synthetic_corpus(self.directory / "corpus.json")
        self.store = store_module.PrivacyReviewStore(self.path, "P01", self.corpus)

    def tearDown(self) -> None:
        shutil.rmtree(self.directory, ignore_errors=True)

    def test_the_identifier_is_derived_without_being_passed(self):
        self.assertIn("锦程", self.store.identifiers)

    def test_a_metadata_only_row_is_separated_from_a_content_row(self):
        metadata_only = self.store.payload(0)["occurrences"]
        self.assertTrue(metadata_only["metadata_only"])
        self.assertFalse(metadata_only["content_exposed"])
        self.assertEqual(["source_document"], metadata_only["fields"])

        content = self.store.payload(1)["occurrences"]
        self.assertTrue(content["content_exposed"])
        self.assertIn("reference_answer", content["fields"])
        self.assertIn("gold_evidence_span", content["fields"])

    def test_a_row_with_no_hit_at_all_is_reported_as_such(self):
        occurrences = self.store.payload(2)["occurrences"]
        self.assertEqual([], occurrences["fields"])
        self.assertFalse(occurrences["content_exposed"])
        self.assertFalse(occurrences["metadata_only"])

    def test_the_evidence_unit_is_resolved_and_the_span_verified(self):
        payload = self.store.payload(0)
        self.assertTrue(payload["unit_resolved"])
        self.assertTrue(payload["span_in_unit"])
        self.assertIn("POST", "".join(run["text"] for run in payload["unit_text"]))

    def test_a_span_that_drifted_from_the_corpus_is_flagged(self):
        workbook = load_workbook(self.path)
        column = HEADERS.index("gold_evidence_span") + 1
        workbook["P01"].cell(row=2, column=column, value="请求方式为 GET")
        workbook.save(self.path)
        store = store_module.PrivacyReviewStore(self.path, "P01", self.corpus)
        self.assertFalse(store.payload(0)["span_in_unit"])

    def test_a_decision_reaches_the_workbook(self):
        self.store.decide(0, "CLEARED", "经授权用于受控实验")
        workbook = load_workbook(self.path)
        sheet = workbook["P01"]
        self.assertEqual("CLEARED", sheet.cell(row=2, column=HEADERS.index("privacy_review_status") + 1).value)
        self.assertEqual(
            "经授权用于受控实验",
            sheet.cell(row=2, column=HEADERS.index("privacy_review_notes") + 1).value,
        )

    def test_clearing_a_row_leaves_the_content_verdict_alone(self):
        self.store.decide(0, "NO_RISK", "")
        workbook = load_workbook(self.path)
        self.assertEqual(
            "APPROVED",
            workbook["P01"].cell(row=2, column=HEADERS.index("approval_status") + 1).value,
        )

    def test_a_privacy_rejection_also_rejects_the_content(self):
        # import_review_workbooks refuses a privacy rejection that leaves the
        # content approved, so the store has to carry the consequence.
        self.store.decide(1, "REJECTED", "客户主体信息无法脱敏")
        workbook = load_workbook(self.path)
        sheet = workbook["P01"]
        self.assertEqual("REJECTED", sheet.cell(row=3, column=HEADERS.index("approval_status") + 1).value)
        self.assertEqual("REJECTED", sheet.cell(row=3, column=HEADERS.index("privacy_review_status") + 1).value)

    def test_anonymized_without_a_substitution_rule_is_refused(self):
        with self.assertRaisesRegex(ValueError, "substitution rule"):
            self.store.decide(1, "ANONYMIZED", "   ")

    def test_a_rejection_without_a_reason_is_refused(self):
        with self.assertRaisesRegex(ValueError, "requires a reason"):
            self.store.decide(1, "REJECTED", "")

    def test_pending_is_not_a_decision(self):
        with self.assertRaisesRegex(ValueError, "unsupported"):
            self.store.decide(0, "PENDING", "")

    def test_a_vanished_workbook_is_not_silently_recreated(self):
        self.path.unlink()
        with self.assertRaisesRegex(ValueError, "no longer exists"):
            self.store.decide(0, "CLEARED", "")
        self.assertFalse(self.path.exists())

    def test_progress_counts_decisions_and_content_exposure(self):
        self.store.decide(0, "NO_RISK", "")
        progress = self.store.decide(1, "CLEARED", "经授权")
        self.assertEqual(3, progress["total"])
        self.assertEqual(2, progress["decided"])
        self.assertEqual(1, progress["remaining"])
        self.assertEqual(2, progress["cleared"])
        self.assertEqual(1, progress["content_exposed"])

    def test_next_undecided_skips_finished_rows_and_wraps(self):
        self.store.decide(1, "NO_RISK", "")
        self.assertEqual(2, self.store.next_undecided(1))
        self.store.decide(0, "NO_RISK", "")
        self.store.decide(2, "NO_RISK", "")
        self.assertIsNone(self.store.next_undecided(0))

    def test_a_missing_sheet_is_named_in_the_error(self):
        with self.assertRaisesRegex(ValueError, "no sheet named P99"):
            store_module.PrivacyReviewStore(self.path, "P99", self.corpus)


class ValidationParityTests(unittest.TestCase):
    """A decision this tool accepts must be one the import gate accepts."""

    def setUp(self) -> None:
        self.importer = load("import_review_workbooks")

    def test_the_status_vocabulary_matches_the_import_gate(self):
        self.assertEqual(
            self.importer.PRIVACY_STATUSES - {"PENDING"},
            set(store_module.PRIVACY_DECISIONS),
        )

    def test_the_cleared_set_matches_the_import_gate(self):
        self.assertEqual(self.importer.PRIVACY_CLEARED, store_module.PRIVACY_CLEARED)

    def test_the_statuses_needing_a_reason_match_the_import_gate(self):
        self.assertEqual({"ANONYMIZED", "REJECTED"}, store_module.REASON_REQUIRED)

    def test_span_comparison_matches_the_finalization_gate(self):
        # A stricter comparison would raise phantom mismatches on rows the gate
        # accepts, so the two normalizations must agree character for character.
        finalize = load("finalize_qa_review")
        for text in ("1）请求方式：POST", " 前后空白 ", "换行\n与\t制表", ""):
            self.assertEqual(finalize.normalize(text), store_module.normalize(text))


class ServerSmokeTests(unittest.TestCase):
    """The server must serve a candidate and persist a decision end to end."""

    @classmethod
    def setUpClass(cls) -> None:
        cls.directory = Path(tempfile.mkdtemp())
        cls.path = synthetic_workbook(cls.directory / "privacy.xlsx")
        cls.corpus = synthetic_corpus(cls.directory / "corpus.json")
        cls.port = 8897
        cls.process = subprocess.Popen(
            [
                sys.executable,
                str(ROOT / "scripts/serve_privacy_review.py"),
                "--workbook",
                str(cls.path),
                "--corpus",
                str(cls.corpus),
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
        raise RuntimeError("privacy review server did not start")

    @classmethod
    def tearDownClass(cls) -> None:
        cls.process.terminate()
        cls.process.wait(timeout=10)
        shutil.rmtree(cls.directory, ignore_errors=True)

    def get(self, route: str) -> dict:
        return json.loads(urllib.request.urlopen(f"{self.base}{route}", timeout=5).read())

    def post(self, route: str, body: dict) -> dict:
        request = urllib.request.Request(
            f"{self.base}{route}",
            data=json.dumps(body).encode("utf-8"),
            headers={"Content-Type": "application/json"},
        )
        return json.loads(urllib.request.urlopen(request, timeout=5).read())

    def test_index_and_assets_are_served(self):
        page = urllib.request.urlopen(f"{self.base}/", timeout=5).read().decode("utf-8")
        self.assertIn("隐私放行复核", page)
        script = urllib.request.urlopen(f"{self.base}/app.js", timeout=5).read().decode("utf-8")
        self.assertIn("ANONYMIZED", script)

    def test_state_starts_at_an_undecided_candidate(self):
        payload = self.get("/api/state")
        self.assertEqual("P01", payload["project"])
        self.assertFalse(payload["finished"])
        self.assertEqual("PENDING", payload["candidate"]["privacy_review_status"])

    def test_a_decision_persists_and_advances(self):
        payload = self.post("/api/decide", {"index": 0, "status": "NO_RISK", "notes": "", "next": "undecided"})
        self.assertGreaterEqual(payload["progress"]["decided"], 1)
        self.assertNotEqual(0, payload["candidate"]["index"])

    def test_an_invalid_decision_returns_the_reason(self):
        request = urllib.request.Request(
            f"{self.base}/api/decide",
            data=json.dumps({"index": 1, "status": "ANONYMIZED", "notes": ""}).encode("utf-8"),
            headers={"Content-Type": "application/json"},
        )
        with self.assertRaises(urllib.error.HTTPError) as caught:
            urllib.request.urlopen(request, timeout=5)
        self.assertEqual(400, caught.exception.code)
        self.assertIn("substitution rule", json.loads(caught.exception.read())["error"])

    def test_goto_moves_without_deciding(self):
        payload = self.post("/api/goto", {"index": 2})
        self.assertEqual(2, payload["candidate"]["index"])


@unittest.skipUnless(WORKBOOK.is_file() and CORPUS.is_file(), "real P01 pack is absent")
class RealWorkbookTests(unittest.TestCase):
    """Pin the shape of the actual review that is still outstanding."""

    @classmethod
    def setUpClass(cls) -> None:
        cls.store = store_module.PrivacyReviewStore(WORKBOOK, "P01", CORPUS)

    def test_the_customer_name_is_derived_from_the_real_corpus(self):
        self.assertTrue(self.store.identifiers)

    def test_all_twenty_seven_rows_load(self):
        self.assertEqual(27, self.store.progress()["total"])

    def test_only_two_rows_expose_the_name_in_publishable_content(self):
        # The other 25 are flagged solely through the source file name, which never
        # leaves the workbench. Splitting them is the point of this view.
        exposed = [
            row["qa_id"] for row in self.store.rows if self.store.occurrences(row)["content_exposed"]
        ]
        self.assertEqual(["QA029", "QA035"], sorted(exposed))

    def test_every_row_carries_a_resolvable_span(self):
        unresolved = [
            self.store.payload(index)["qa_id"]
            for index in range(len(self.store.rows))
            if not self.store.payload(index)["span_in_unit"]
        ]
        self.assertEqual([], unresolved)


if __name__ == "__main__":
    unittest.main()
