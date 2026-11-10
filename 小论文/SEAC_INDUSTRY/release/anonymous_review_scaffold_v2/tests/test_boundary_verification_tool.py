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
WORKBOOK = ROOT / "outputs/prelabel_verification_v1/SEAC_boundary_prelabel_verification.xlsx"
HEADERS = [
    "candidate_id",
    "project_id",
    "row_group",
    "source_document",
    "document_type",
    "boundary_stratum",
    "left_section",
    "left_block_type",
    "left_evidence",
    "right_section",
    "right_block_type",
    "right_evidence",
    "human_label",
    "review_status",
    "review_notes",
]


def load(name: str):
    spec = importlib.util.spec_from_file_location(name, ROOT / "scripts" / f"{name}.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


store_module = load("boundary_verification_store")


def synthetic_workbook(path: Path, rows: int = 3) -> Path:
    workbook = Workbook()
    workbook.remove(workbook.active)
    guide = workbook.create_sheet("Guide")
    guide.cell(row=1, column=1, value="SEAC 边界预标人工验证工作簿")
    sheet = workbook.create_sheet("P02")
    for column, header in enumerate(HEADERS, 1):
        sheet.cell(row=1, column=column, value=header)
    for index in range(rows):
        values = {
            "candidate_id": f"P02-BP{index:04d}",
            "project_id": "P02",
            "row_group": "AGREEMENT_SAMPLE" if index else "FLAGGED_REVIEW",
            "source_document": "spec.docx",
            "document_type": "detailed_design",
            "boundary_stratum": "inside_table" if index else "heading_transition",
            "left_section": "系统设计 > 劳保用品管理",
            "left_block_type": "table_row" if index else "heading",
            "left_evidence": (
                "表头: 参数 | 参数 | 类型 数据行: size | size | Long" if index else "逻辑处理"
            ),
            "right_section": "系统设计 > 劳保用品管理",
            "right_block_type": "table_row" if index else "paragraph",
            "right_evidence": (
                "表头: 参数 | 参数 | 类型 数据行: page | page | Int" if index else "同步"
            ),
        }
        for column, header in enumerate(HEADERS, 1):
            sheet.cell(row=index + 2, column=column, value=values.get(header, ""))
    workbook.save(path)
    return path


class TableRenderingTests(unittest.TestCase):
    """A table row must arrive as aligned cells, not as one long string."""

    def test_header_and_data_cells_are_paired(self):
        parsed = store_module.parse_table_evidence("表头: 参数名称 | 类型 | 必须 数据行: size | Long | 是")
        self.assertEqual("table", parsed["kind"])
        self.assertEqual(
            [("参数名称", "size"), ("类型", "Long"), ("必须", "是")],
            [(column["header"], column["value"]) for column in parsed["columns"]],
        )

    def test_merged_cell_repetition_is_collapsed(self):
        parsed = store_module.parse_table_evidence(
            "表头: 售前调研 | 售前调研 | 售前调研 数据行: 项目名称 | 项目名称 | 项目名称"
        )
        self.assertEqual(1, len(parsed["columns"]))
        self.assertEqual(2, parsed["collapsed"])

    def test_ragged_row_is_padded_not_dropped(self):
        parsed = store_module.parse_table_evidence("表头: 参数 | 类型 数据行: size | Long | 备注")
        self.assertEqual(3, len(parsed["columns"]))
        self.assertEqual("", parsed["columns"][2]["header"])
        self.assertEqual("备注", parsed["columns"][2]["value"])

    def test_paragraph_stays_plain_text(self):
        parsed = store_module.parse_table_evidence("本平台响应时间不超过3秒。")
        self.assertEqual({"kind": "text", "text": "本平台响应时间不超过3秒。"}, parsed)


class StoreTests(unittest.TestCase):
    def setUp(self) -> None:
        self.path = synthetic_workbook(Path(tempfile.mkdtemp()) / "verification.xlsx")
        self.store = store_module.VerificationStore(self.path)

    def reload(self) -> store_module.VerificationStore:
        return store_module.VerificationStore(self.path)

    def test_guide_sheet_is_not_a_project(self):
        self.assertEqual(["P02"], self.store.projects)
        self.assertEqual(3, len(self.store.rows))

    def test_decision_is_written_through_to_the_workbook(self):
        self.store.decide(0, "BREAK", "COMPLETE", "")
        sheet = load_workbook(self.path)["P02"]
        self.assertEqual("BREAK", sheet.cell(row=2, column=HEADERS.index("human_label") + 1).value)
        self.assertEqual("COMPLETE", sheet.cell(row=2, column=HEADERS.index("review_status") + 1).value)
        self.assertEqual("BREAK", self.reload().rows[0]["human_label"])

    def test_only_the_three_editable_columns_change(self):
        before = [cell.value for cell in load_workbook(self.path)["P02"][2]]
        self.store.decide(0, "NOISE", "COMPLETE", "页脚残片")
        after = [cell.value for cell in load_workbook(self.path)["P02"][2]]
        self.assertEqual(before[:12], after[:12])
        self.assertEqual(["NOISE", "COMPLETE", "页脚残片"], after[12:15])

    def test_ambiguous_row_requires_a_reason(self):
        with self.assertRaisesRegex(ValueError, "needs a reason"):
            self.store.decide(0, "BREAK", "NEEDS_DISCUSSION", "   ")

    def test_ambiguous_row_with_a_reason_is_accepted(self):
        self.store.decide(0, "BREAK", "NEEDS_DISCUSSION", "标题与首段关系不明")
        self.assertEqual(1, self.store.progress()["flagged"])

    def test_unsupported_label_is_refused(self):
        with self.assertRaisesRegex(ValueError, "unsupported label"):
            self.store.decide(0, "SPLIT", "COMPLETE", "")

    def test_a_vanished_workbook_is_not_silently_recreated(self):
        self.path.unlink()
        with self.assertRaisesRegex(ValueError, "no longer exists"):
            self.store.decide(0, "BREAK", "COMPLETE", "")
        self.assertFalse(self.path.exists())

    def test_progress_counts_only_valid_decisions(self):
        self.store.decide(0, "BREAK", "COMPLETE", "")
        progress = self.store.decide(1, "NO_BREAK", "COMPLETE", "")
        self.assertEqual({"total": 3, "decided": 2, "flagged": 0}, progress["projects"]["P02"])
        self.assertEqual(1, progress["remaining"])
        self.assertEqual({"BREAK": 1, "NO_BREAK": 1, "NOISE": 0}, progress["label_counts"])

    def test_next_undecided_skips_finished_rows_and_wraps(self):
        self.store.decide(1, "NO_BREAK", "COMPLETE", "")
        self.assertEqual(2, self.store.next_undecided(1))
        self.store.decide(0, "BREAK", "COMPLETE", "")
        self.store.decide(2, "BREAK", "COMPLETE", "")
        self.assertIsNone(self.store.next_undecided(0))

    def test_payload_hides_nothing_the_annotator_needs_and_adds_no_prelabel(self):
        payload = self.store.payload(0)
        self.assertEqual("P02-BP0000", payload["candidate_id"])
        self.assertEqual("heading", payload["left"]["block_type"])
        self.assertTrue(payload["same_section"])
        self.assertNotIn("prelabel", json.dumps(payload))


class ServerSmokeTests(unittest.TestCase):
    """The server must serve a candidate and persist a decision end to end."""

    @classmethod
    def setUpClass(cls) -> None:
        cls.directory = Path(tempfile.mkdtemp())
        cls.path = synthetic_workbook(cls.directory / "verification.xlsx")
        cls.port = 8899
        cls.process = subprocess.Popen(
            [
                sys.executable,
                str(ROOT / "scripts/serve_boundary_verification.py"),
                "--workbook",
                str(cls.path),
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
        raise RuntimeError("annotation server did not start")

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
        self.assertIn("另起一个新的", page)
        self.assertIn("BREAK", urllib.request.urlopen(f"{self.base}/app.js", timeout=5).read().decode("utf-8"))

    def test_state_starts_at_an_undecided_candidate(self):
        state = self.get("/api/state")
        self.assertEqual(3, state["progress"]["total"])
        self.assertFalse(state["finished"])
        # Resuming must land on work still to do, whatever earlier tests decided.
        self.assertEqual("", state["candidate"]["human_label"])

    def test_decision_persists_and_advances(self):
        payload = self.post("/api/decide", {"index": 0, "label": "BREAK", "status": "COMPLETE", "notes": "", "next": "undecided"})
        self.assertEqual(1, payload["progress"]["decided"])
        self.assertEqual(1, payload["candidate"]["index"])
        sheet = load_workbook(self.path)["P02"]
        self.assertEqual("BREAK", sheet.cell(row=2, column=13).value)

    def test_invalid_decision_is_rejected_with_a_reason(self):
        with self.assertRaises(urllib.error.HTTPError) as caught:
            self.post("/api/decide", {"index": 2, "label": "BREAK", "status": "NEEDS_DISCUSSION", "notes": ""})
        self.assertEqual(400, caught.exception.code)
        self.assertIn("needs a reason", json.loads(caught.exception.read())["error"])


class RealWorkbookTests(unittest.TestCase):
    """Guard the tool against the shape of the workbook actually shipped."""

    def setUp(self) -> None:
        if not WORKBOOK.is_file():
            self.skipTest("verification workbook is not present")
        self.path = Path(tempfile.mkdtemp()) / WORKBOOK.name
        shutil.copy(WORKBOOK, self.path)

    def test_every_row_of_the_shipped_pack_loads_and_renders(self):
        store = store_module.VerificationStore(self.path)
        self.assertEqual(1050, len(store.rows))
        self.assertEqual(9, len(store.projects))
        for index in range(len(store.rows)):
            payload = store.payload(index)
            self.assertTrue(payload["candidate_id"])
            for side in ("left", "right"):
                evidence = payload[side]["evidence"]
                self.assertIn(evidence["kind"], {"text", "table"})
                if evidence["kind"] == "table":
                    self.assertTrue(evidence["columns"])

    def test_a_decision_on_the_real_pack_survives_a_reload(self):
        """Works whether the shipped pack is still blank or already filled in."""

        store = store_module.VerificationStore(self.path)
        undecided_before = store.progress()["remaining"]
        was_decided = store.is_decided(store.rows[0])
        store.decide(0, "NO_BREAK", "COMPLETE", "同表相邻数据行")
        reloaded = store_module.VerificationStore(self.path)
        self.assertEqual("NO_BREAK", reloaded.rows[0]["human_label"])
        self.assertEqual("同表相邻数据行", reloaded.rows[0]["review_notes"])
        expected = undecided_before if was_decided else undecided_before - 1
        self.assertEqual(expected, reloaded.progress()["remaining"])


if __name__ == "__main__":
    unittest.main()
