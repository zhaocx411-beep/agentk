import importlib.util
import unittest
from pathlib import Path


SCRIPT_PATH = Path(__file__).parents[1] / "scripts" / "align_qa_evidence_units.py"
SPEC = importlib.util.spec_from_file_location("align_qa_evidence_units", SCRIPT_PATH)
alignment = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(alignment)


class QaAlignmentTests(unittest.TestCase):
    def test_unique_table_row_alignment(self):
        block = {
            "block_id": "T1",
            "rows": [["字段", "类型"], ["name", "varchar"], ["id", "number"]],
        }
        item = {"qa_id": "Q1", "gold_evidence_span": "name | varchar"}
        result = alignment.align_item(item, block)
        self.assertEqual("T1#R002", result["gold_evidence_unit_id"])
        self.assertEqual("UNIQUE_EXACT_ROW", result["unit_alignment_status"])

    def test_non_table_block_is_its_own_unit(self):
        result = alignment.align_item(
            {"qa_id": "Q1", "gold_evidence_span": "正文"},
            {"block_id": "B1", "text": "正文"},
        )
        self.assertEqual("B1", result["gold_evidence_unit_id"])


if __name__ == "__main__":
    unittest.main()
