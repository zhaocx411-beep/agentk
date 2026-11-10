import csv
import importlib.util
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


SCRIPT_PATH = Path(__file__).parents[1] / "scripts" / "run_chunking_experiment.py"
sys.path.insert(0, str(SCRIPT_PATH.parents[1]))
SPEC = importlib.util.spec_from_file_location("run_chunking_experiment", SCRIPT_PATH)
runner = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(runner)


class RunnerGateTests(unittest.TestCase):
    def question(self, privacy_status="NO_RISK"):
        return {
            "qa_id": "Q1",
            "question": "请求方式是什么？",
            "reference_answer": "POST",
            "gold_evidence_id": "B1",
            "gold_evidence_unit_id": "B1",
            "gold_evidence_span": "POST",
            "audit_status": "KEEP",
            "privacy_review_status": privacy_status,
        }

    def test_formal_run_requires_privacy_clearance(self):
        with self.assertRaisesRegex(ValueError, "lack privacy clearance"):
            runner.validate_questions([self.question("PENDING")])

    def test_exploratory_run_may_bypass_review_gates(self):
        runner.validate_questions([self.question("PENDING")], allow_unreviewed=True)

    def test_hierarchical_summary_counts_all_indexed_vectors(self):
        class Chunk:
            def __init__(self, level):
                self.token_count = 10
                self.metadata = {"retrieval_level": level}

        summary = runner.summarize([Chunk("parent"), Chunk("child"), Chunk("child")])
        self.assertEqual(1, summary["parent_chunk_count"])
        self.assertEqual(2, summary["child_chunk_count"])
        self.assertEqual(3, summary["retrieval_chunk_count"])


class WithheldInputTests(unittest.TestCase):
    def test_none_marks_an_axis_as_withheld(self):
        for value in ("none", "NONE", " none ", ""):
            with self.subTest(value=value):
                self.assertTrue(runner.is_withheld(value))

    def test_a_real_path_is_not_withheld(self):
        self.assertFalse(runner.is_withheld("workbench/P10/qa_approved.json"))


class SingleAxisRunTests(unittest.TestCase):
    """A project withheld from one axis still has to produce the other one.

    P08 has no promoted boundary file and P10 is withheld from retrieval, so the
    runner is exercised in both shapes end to end.
    """

    ROOT = Path(__file__).parents[1]

    def setUp(self) -> None:
        self.directory = tempfile.TemporaryDirectory()
        root = Path(self.directory.name)
        corpus = [
            {
                "file_name": "spec.docx",
                "document_type": "detailed_design",
                "blocks": [
                    {
                        "block_id": f"spec:B{index:04d}",
                        "block_type": "paragraph",
                        "heading_path": "接口设计",
                        "text": f"第{index}项要求的响应时间不超过{index}秒。",
                    }
                    for index in range(1, 9)
                ],
            }
        ]
        self.corpus = root / "corpus.json"
        self.corpus.write_text(json.dumps(corpus, ensure_ascii=False), encoding="utf-8")
        self.qa = root / "qa.json"
        self.qa.write_text(
            json.dumps(
                [
                    {
                        "qa_id": "Q1", "project_id": "PX",
                        "question": "第3项要求的响应时间是多少？",
                        "reference_answer": "不超过3秒",
                        "gold_evidence_id": "spec:B0003",
                        "gold_evidence_unit_id": "spec:B0003",
                        "gold_evidence_span": "第3项要求的响应时间不超过3秒。",
                        "audit_status": "KEEP", "privacy_review_status": "NO_RISK",
                    }
                ],
                ensure_ascii=False,
            ),
            encoding="utf-8",
        )
        self.breakpoints = root / "bp.json"
        self.breakpoints.write_text(
            json.dumps(
                [
                    {
                        "candidate_id": "C1", "document": "spec.docx",
                        "left_unit_id": "spec:B0003", "right_unit_id": "spec:B0004",
                        "human_label": "BREAK",
                    }
                ],
                ensure_ascii=False,
            ),
            encoding="utf-8",
        )
        self.output = root / "out"

    def tearDown(self) -> None:
        self.directory.cleanup()

    def run_experiment(self, qa: str, breakpoints: str) -> subprocess.CompletedProcess:
        return subprocess.run(
            [
                sys.executable,
                str(self.ROOT / "scripts/run_chunking_experiment.py"),
                "--project-id", "PX",
                "--corpus", str(self.corpus),
                "--qa", qa,
                "--breakpoints", breakpoints,
                "--methods", "fixed",
                "--encoder", "tfidf",
                "--output-dir", str(self.output),
            ],
            capture_output=True,
            text=True,
        )

    def results(self) -> dict:
        return json.loads((self.output / "results.json").read_text(encoding="utf-8"))["fixed"]

    def csv_row(self) -> dict:
        with (self.output / "results.csv").open(encoding="utf-8-sig") as handle:
            return next(iter(csv.DictReader(handle)))

    def test_withholding_questions_keeps_the_boundary_result(self):
        completed = self.run_experiment("none", str(self.breakpoints))
        self.assertEqual(0, completed.returncode, completed.stderr)
        result = self.results()
        self.assertIsNone(result["retrieval"])
        self.assertIsNotNone(result["boundary"])
        self.assertFalse((self.output / "fixed_retrieval_details.json").exists())
        self.assertTrue((self.output / "fixed_boundary_details.json").is_file())

    def test_withholding_boundaries_keeps_the_retrieval_result(self):
        completed = self.run_experiment(str(self.qa), "none")
        self.assertEqual(0, completed.returncode, completed.stderr)
        result = self.results()
        self.assertIsNone(result["boundary"])
        self.assertIsNotNone(result["retrieval"])
        self.assertTrue((self.output / "fixed_retrieval_details.json").is_file())
        self.assertFalse((self.output / "fixed_boundary_details.json").exists())

    def test_a_withheld_axis_writes_empty_columns_rather_than_zeros(self):
        """A zero would enter the analysis as a real score."""

        self.run_experiment("none", str(self.breakpoints))
        row = self.csv_row()
        self.assertEqual("", row["mrr"])
        self.assertEqual("", row["query_latency_ms_mean"])
        self.assertNotEqual("", row["boundary_f1"])

    def test_the_config_records_which_axis_was_withheld(self):
        self.run_experiment("none", str(self.breakpoints))
        config = json.loads((self.output / "config.json").read_text(encoding="utf-8"))
        self.assertTrue(config["retrieval_withheld"])
        self.assertFalse(config["boundary_withheld"])
        self.assertIsNone(config["qa"])
        self.assertIsNone(config["qa_sha256"])
        self.assertEqual("PX", config["project_id"])

    def test_withholding_both_axes_is_refused(self):
        completed = self.run_experiment("none", "none")
        self.assertNotEqual(0, completed.returncode)
        self.assertIn("nothing to evaluate", completed.stderr)

    def test_the_analysis_reads_a_withheld_retrieval_run(self):
        """The withheld project contributes boundary rows and no retrieval rows."""

        spec = importlib.util.spec_from_file_location(
            "analyze_formal_results", self.ROOT / "scripts/analyze_formal_results.py"
        )
        analysis = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(analysis)
        self.run_experiment("none", str(self.breakpoints))
        _, retrieval_rows, boundary_rows, efficiency_rows, _ = analysis.load_run(
            self.output, self.ROOT, allow_exploratory=True
        )
        self.assertEqual([], retrieval_rows)
        self.assertEqual([], efficiency_rows)
        self.assertTrue(boundary_rows)
        self.assertEqual({"PX"}, {row["project_id"] for row in boundary_rows})


if __name__ == "__main__":
    unittest.main()
