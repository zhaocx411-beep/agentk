import importlib.util
import unittest
from pathlib import Path


SCRIPT_PATH = Path(__file__).parents[1] / "scripts" / "adjudicate_boundary_annotations.py"
SPEC = importlib.util.spec_from_file_location("adjudicate_boundary_annotations", SCRIPT_PATH)
adjudication = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(adjudication)


class AdjudicationTests(unittest.TestCase):
    def test_merge_preserves_agreement_and_flags_disagreement(self):
        common = {"source_document": "doc", "left_unit_id": "B1", "right_unit_id": "B2"}
        records_a = [
            {**common, "candidate_id": "C1", "human_label": "BREAK"},
            {**common, "candidate_id": "C2", "human_label": "NO_BREAK"},
        ]
        records_b = [
            {**common, "candidate_id": "C1", "human_label": "BREAK"},
            {**common, "candidate_id": "C2", "human_label": "NOISE"},
        ]
        merged, summary = adjudication.merge_annotations(records_a, records_b)
        self.assertEqual(1, summary["agreement_count"])
        self.assertEqual("BREAK", merged[0]["human_label"])
        self.assertEqual("NEEDS_ADJUDICATION", merged[1]["review_status"])

    def test_finalize_requires_adjudication_label(self):
        with self.assertRaisesRegex(ValueError, "unresolved"):
            adjudication.finalize([{"candidate_id": "C1", "agreement": False, "adjudication_label": ""}])

    def test_perfect_kappa(self):
        self.assertEqual(1.0, adjudication.cohen_kappa(["BREAK", "NO_BREAK"], ["BREAK", "NO_BREAK"]))


if __name__ == "__main__":
    unittest.main()
