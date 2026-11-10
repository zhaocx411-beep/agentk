import importlib.util
import sys
import unittest
from pathlib import Path


SCRIPT_PATH = Path(__file__).parents[1] / "scripts" / "analyze_formal_results.py"
sys.path.insert(0, str(SCRIPT_PATH.parent))
SPEC = importlib.util.spec_from_file_location("analyze_formal_results", SCRIPT_PATH)
analysis = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(analysis)


class FormalAnalysisTests(unittest.TestCase):
    def test_project_id_comes_from_config_when_retrieval_is_withheld(self):
        project_id = analysis.resolve_run_project_id(
            {"project_id": "P10"},
            [],
            [{"project_id": "P10", "candidate_id": "B1"}],
        )
        self.assertEqual("P10", project_id)

    def test_project_id_rejects_result_rows_from_another_project(self):
        with self.assertRaisesRegex(ValueError, "disagrees with result rows"):
            analysis.resolve_run_project_id(
                {"project_id": "P10"},
                [],
                [{"project_id": "P09", "candidate_id": "B1"}],
            )

    def test_retrieval_summary_counts_misses_and_spans(self):
        rows = [
            {"rank": 1, "span_rank": 2, "has_gold_span": True},
            {"rank": None, "span_rank": None, "has_gold_span": True},
        ]
        result = analysis.retrieval_summary(rows)
        self.assertEqual(2, result["question_count"])
        self.assertAlmostEqual(0.5, result["mrr"])
        self.assertAlmostEqual(0.25, result["span_mrr"])
        self.assertAlmostEqual(0.5, result["hit_rate@5"])

    def test_boundary_summary(self):
        rows = [
            {"human_label": "BREAK", "predicted_label": "BREAK"},
            {"human_label": "BREAK", "predicted_label": "NO_BREAK"},
            {"human_label": "NO_BREAK", "predicted_label": "BREAK"},
            {"human_label": "NO_BREAK", "predicted_label": "NO_BREAK"},
        ]
        result = analysis.boundary_summary(rows)
        self.assertEqual(1, result["tp"])
        self.assertEqual(1, result["fp"])
        self.assertEqual(1, result["fn"])
        self.assertAlmostEqual(0.5, result["f1"])

    def test_holm_adjustment_is_monotone_in_sorted_order(self):
        adjusted = analysis.holm_adjust([0.01, 0.04, 0.03])
        self.assertEqual([0.03, 0.06, 0.06], adjusted)

    def test_paired_difference_favors_primary(self):
        primary = [
            {"project_id": "P1", "qa_id": "Q1", "rank": 1, "span_rank": 1, "has_gold_span": True},
            {"project_id": "P1", "qa_id": "Q2", "rank": 2, "span_rank": 2, "has_gold_span": True},
        ]
        baseline = [
            {"project_id": "P1", "qa_id": "Q1", "rank": 2, "span_rank": 2, "has_gold_span": True},
            {"project_id": "P1", "qa_id": "Q2", "rank": None, "span_rank": None, "has_gold_span": True},
        ]
        result = analysis.paired_retrieval(primary, baseline, 100, 7)
        self.assertGreater(result[0]["mean_difference"], 0)
        self.assertEqual(2, result[0]["pair_count"])
        self.assertEqual("project_id", result[0]["randomization_unit"])
        self.assertEqual("exact_cluster_enumeration", result[0]["randomization_mode"])
        self.assertEqual(2, result[0]["randomization_permutations"])

    def test_randomization_flips_whole_projects_not_individual_questions(self):
        pairs = [
            {"project_id": "P1", "difference": 1.0},
            {"project_id": "P1", "difference": 1.0},
            {"project_id": "P2", "difference": 1.0},
        ]
        result = analysis.clustered_sign_flip_pvalue(pairs, 1000, 7)
        self.assertEqual(2, result["cluster_count"])
        self.assertEqual(4, result["randomization_permutations"])
        self.assertAlmostEqual(0.5, result["p_value"])

    def test_analyze_separates_confirmatory_test_from_secondary_holm_family(self):
        methods = ["proposed_v6", "llamaindex_sentence", "fixed"]
        retrieval_rows = []
        boundary_rows = []
        for project_index, project in enumerate(("P04", "P05")):
            for method_index, method in enumerate(methods):
                rank = 1 if method == "proposed_v6" else 2 + method_index
                retrieval_rows.append(
                    {
                        "project_id": project,
                        "qa_id": f"Q{project_index}",
                        "method": method,
                        "rank": rank,
                        "span_rank": rank,
                        "has_gold_span": True,
                        "question_type": "fact",
                    }
                )
                boundary_rows.append(
                    {
                        "project_id": project,
                        "candidate_id": f"B{project_index}",
                        "method": method,
                        "human_label": "BREAK",
                        "predicted_label": "BREAK" if method == "proposed_v6" else "NO_BREAK",
                        "boundary_stratum": "paragraph",
                        "document_type": "requirements",
                    }
                )
        result = analysis.analyze(
            retrieval_rows,
            boundary_rows,
            "proposed_v6",
            "llamaindex_sentence",
            20,
            7,
        )
        confirmatory = result["paired_retrieval_comparisons"]["llamaindex_sentence"][0]
        self.assertEqual("mrr", confirmatory["metric"])
        self.assertTrue(confirmatory["confirmatory_test"])
        self.assertIsNone(confirmatory["p_value_holm_secondary"])
        self.assertEqual(7, result["retrieval_holm_family_size"])
        secondary = [
            item
            for comparisons in result["paired_retrieval_comparisons"].values()
            for item in comparisons
            if not item["confirmatory_test"]
        ]
        self.assertEqual(7, len(secondary))
        self.assertTrue(all("p_value_holm_secondary" in item for item in secondary))

    def test_paired_boundary_difference_favors_primary(self):
        primary = [
            {"project_id": "P1", "candidate_id": "B1", "human_label": "BREAK", "predicted_label": "BREAK"},
            {"project_id": "P1", "candidate_id": "B2", "human_label": "NO_BREAK", "predicted_label": "NO_BREAK"},
        ]
        baseline = [
            {"project_id": "P1", "candidate_id": "B1", "human_label": "BREAK", "predicted_label": "NO_BREAK"},
            {"project_id": "P1", "candidate_id": "B2", "human_label": "NO_BREAK", "predicted_label": "BREAK"},
        ]
        result = analysis.paired_boundary(primary, baseline, 100, 7)
        self.assertGreater(result["f1_difference"], 0)
        self.assertEqual(2, result["pair_count"])
        self.assertEqual("project_id", result["randomization_unit"])
        self.assertEqual(2, result["randomization_permutations"])

    def test_efficiency_uses_all_vectors_and_question_weighted_latency(self):
        rows = [
            {
                "project_id": "P04",
                "method": "proposed_v6",
                "question_count": 2,
                "parent_chunk_count": 10,
                "retrieval_vector_count": 30,
                "chunking_seconds": 1.5,
                "index_seconds": 2.5,
                "query_latency_ms_mean": 10.0,
                "query_latencies_ms": [5.0, 15.0],
            },
            {
                "project_id": "P05",
                "method": "proposed_v6",
                "question_count": 1,
                "parent_chunk_count": 20,
                "retrieval_vector_count": 50,
                "chunking_seconds": 3.5,
                "index_seconds": 4.5,
                "query_latency_ms_mean": 40.0,
                "query_latencies_ms": [40.0],
            },
        ]
        result = analysis.aggregate_efficiency(rows)["proposed_v6"]
        self.assertEqual(30, result["parent_chunk_count"])
        self.assertEqual(80, result["retrieval_vector_count"])
        self.assertAlmostEqual(20.0, result["query_latency_ms_mean"])
        self.assertAlmostEqual(15.0, result["query_latency_ms_p50"])
        self.assertAlmostEqual(37.5, result["query_latency_ms_p95"])


if __name__ == "__main__":
    unittest.main()
