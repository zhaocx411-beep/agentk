import importlib.util
import json
import sys
import tempfile
import unittest
from pathlib import Path


SCRIPT_PATH = Path(__file__).parents[1] / "scripts" / "audit_formal_readiness.py"
sys.path.insert(0, str(SCRIPT_PATH.parent))
SPEC = importlib.util.spec_from_file_location("audit_formal_readiness", SCRIPT_PATH)
readiness = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(readiness)


class FormalReadinessTests(unittest.TestCase):
    def test_ready_project_requires_all_three_artifacts_and_privacy(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "corpus.json").write_text(json.dumps([{"file_name": "doc"}]), encoding="utf-8")
            (root / "bp.json").write_text(json.dumps([{"human_label": "BREAK"}]), encoding="utf-8")
            qa = [{
                "qa_id": "Q1", "project_id": "PX", "audit_status": "KEEP",
                "privacy_review_status": "NO_RISK", "gold_evidence_unit_id": "B1",
                "gold_evidence_span": "evidence",
            }]
            (root / "qa.json").write_text(json.dumps(qa), encoding="utf-8")
            project = {
                "project_id": "PX", "split": "development", "corpus": "corpus.json",
                "breakpoints": "bp.json", "qa": "qa.json", "expected_boundary_count": 1,
                "minimum_qa_count": 1,
            }
            result = readiness.audit_project(project, root)
            self.assertTrue(result["formal_ready"])

    def test_pending_privacy_blocks_project(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "corpus.json").write_text("[]", encoding="utf-8")
            (root / "bp.json").write_text("[]", encoding="utf-8")
            (root / "qa.json").write_text(json.dumps([{"project_id": "PX", "privacy_review_status": "PENDING"}]), encoding="utf-8")
            project = {
                "project_id": "PX", "split": "development", "corpus": "corpus.json",
                "breakpoints": "bp.json", "qa": "qa.json", "expected_boundary_count": 1,
                "minimum_qa_count": 1,
            }
            result = readiness.audit_project(project, root)
            self.assertFalse(result["formal_ready"])
            self.assertTrue(any("privacy" in item for item in result["blockers"]))


class WithheldQaTests(unittest.TestCase):
    """A project below the question minimum may be withheld, but only on the record."""

    def build(self, root: Path, question_count: int, **extra) -> dict:
        (root / "corpus.json").write_text(json.dumps([{"file_name": "doc"}]), encoding="utf-8")
        (root / "bp.json").write_text(json.dumps([{"human_label": "BREAK"}]), encoding="utf-8")
        questions = [
            {
                "qa_id": f"Q{index}", "project_id": "PX", "audit_status": "KEEP",
                "privacy_review_status": "NO_RISK", "gold_evidence_unit_id": "B1",
                "gold_evidence_span": "evidence",
            }
            for index in range(question_count)
        ]
        (root / "qa.json").write_text(json.dumps(questions), encoding="utf-8")
        return {
            "project_id": "PX", "split": "development", "corpus": "corpus.json",
            "breakpoints": "bp.json", "qa": "qa.json", "expected_boundary_count": 1,
            "minimum_qa_count": 3, **extra,
        }

    def test_below_minimum_blocks_when_it_is_not_declared(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            result = readiness.audit_project(self.build(root, 2), root)
            self.assertFalse(result["formal_ready"])
            self.assertTrue(any("below minimum" in item for item in result["blockers"]))

    def test_a_declared_shortfall_passes_and_keeps_its_questions(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            project = self.build(
                root, 2, qa_withheld=True, qa_withheld_reason="2 of 5 survived approval"
            )
            result = readiness.audit_project(project, root)
            self.assertTrue(result["formal_ready"], result["blockers"])
            self.assertTrue(result["qa_withheld"])
            self.assertEqual(2, result["counts"]["qa"])

    def test_a_declaration_without_a_reason_blocks(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            result = readiness.audit_project(self.build(root, 2, qa_withheld=True), root)
            self.assertFalse(result["formal_ready"])
            self.assertIn("qa_withheld is set without qa_withheld_reason", result["blockers"])

    def test_the_flag_cannot_hide_a_project_that_actually_passes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            project = self.build(root, 5, qa_withheld=True, qa_withheld_reason="stated")
            result = readiness.audit_project(project, root)
            self.assertFalse(result["formal_ready"])
            self.assertTrue(any("meets the minimum" in item for item in result["blockers"]))

    def test_withholding_questions_leaves_the_boundary_axis_alone(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            project = self.build(root, 2, qa_withheld=True, qa_withheld_reason="stated")
            result = readiness.audit_project(project, root)
            self.assertFalse(result["boundary_withheld"])
            self.assertEqual(1, result["counts"]["boundaries"])


class RealManifestTests(unittest.TestCase):
    """Pin the two declared exclusions in the frozen manifest."""

    def setUp(self) -> None:
        path = Path(__file__).parents[1] / "configs/formal_run_manifest.json"
        self.projects = {
            item["project_id"]: item
            for item in json.loads(path.read_text(encoding="utf-8"))["projects"]
        }

    def test_p08_withholds_boundaries_only(self):
        self.assertTrue(self.projects["P08"]["boundary_withheld"])
        self.assertFalse(self.projects["P08"].get("qa_withheld"))
        self.assertIn("kappa", self.projects["P08"]["boundary_withheld_reason"])

    def test_p10_withholds_questions_only(self):
        self.assertTrue(self.projects["P10"]["qa_withheld"])
        self.assertFalse(self.projects["P10"].get("boundary_withheld"))
        self.assertIn("§6.1", self.projects["P10"]["qa_withheld_reason"])

    def test_no_other_project_is_withheld(self):
        withheld = {
            key
            for key, item in self.projects.items()
            if item.get("boundary_withheld") or item.get("qa_withheld")
        }
        self.assertEqual({"P08", "P10"}, withheld)


if __name__ == "__main__":
    unittest.main()
