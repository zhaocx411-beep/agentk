from __future__ import annotations

import json
import unittest
from pathlib import Path

from seac_chunking import load_corpus


ROOT = Path(__file__).parents[1]


class SyntheticArtifactTests(unittest.TestCase):
    def test_synthetic_boundaries_are_adjacent_real_units(self) -> None:
        corpus = load_corpus(ROOT / "examples/synthetic/corpus.json")
        units = next(iter(corpus.values()))
        adjacent = {(left.unit_id, right.unit_id) for left, right in zip(units, units[1:])}
        boundaries = json.loads(
            (ROOT / "examples/synthetic/breakpoints.json").read_text(encoding="utf-8")
        )
        self.assertEqual(7, len(units))
        self.assertEqual(6, len(boundaries))
        self.assertTrue(
            all((item["left_unit_id"], item["right_unit_id"]) in adjacent for item in boundaries)
        )

    def test_synthetic_qa_has_exact_approved_private_safe_evidence(self) -> None:
        questions = json.loads((ROOT / "examples/synthetic/qa.json").read_text(encoding="utf-8"))
        self.assertTrue(questions)
        for item in questions:
            self.assertEqual("KEEP", item["audit_status"])
            self.assertEqual("NO_RISK", item["privacy_review_status"])
            self.assertTrue(item["gold_evidence_unit_id"])
            self.assertTrue(item["gold_evidence_span"])

    def test_public_schemas_are_valid_json_schema_documents(self) -> None:
        for path in sorted((ROOT / "schemas").glob("*.schema.json")):
            schema = json.loads(path.read_text(encoding="utf-8"))
            self.assertEqual("https://json-schema.org/draft/2020-12/schema", schema["$schema"])
            self.assertIn("type", schema)


if __name__ == "__main__":
    unittest.main()
