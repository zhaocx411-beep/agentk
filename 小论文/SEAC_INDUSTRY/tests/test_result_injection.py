from __future__ import annotations

import importlib.util
import json
import shutil
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).parents[1]
SCRIPT_PATH = ROOT / "scripts" / "fill_manuscript_results.py"
sys.path.insert(0, str(SCRIPT_PATH.parent))
SPEC = importlib.util.spec_from_file_location("fill_manuscript_results", SCRIPT_PATH)
injector = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(injector)


class ResultInjectionTests(unittest.TestCase):
    def manifest(self) -> dict:
        return json.loads((ROOT / "configs/formal_run_manifest.json").read_text(encoding="utf-8"))

    def analysis(self, significant: bool = True) -> dict:
        manifest = self.manifest()
        projects = [f"P{index:02d}" for index in range(4, 11)]
        retrieval_projects = [project for project in projects if project != "P10"]
        boundary_projects = [project for project in projects if project != "P08"]
        retrieval = {}
        boundary = {}
        efficiency = {}
        comparisons = {}
        boundary_comparisons = {}
        for method_index, method in enumerate(manifest["methods"]):
            is_primary = method == "proposed_v6"
            is_ablation = method.startswith("seac_no_")
            base_mrr = 0.60 + method_index * 0.002
            if is_primary:
                base_mrr = 0.72
            elif is_ablation:
                base_mrr = 0.68 - method_index * 0.0005
            base_f1 = 0.65 + method_index * 0.002
            if is_primary:
                base_f1 = 0.80
            retrieval[method] = {
                "overall_micro": {
                    "question_count": 70,
                    "mrr": base_mrr,
                    "mrr_ci95": [base_mrr - 0.02, base_mrr + 0.02],
                    "hit_rate@1": min(base_mrr - 0.05, 1.0),
                    "hit_rate@3": min(base_mrr + 0.10, 1.0),
                    "hit_rate@5": min(base_mrr + 0.15, 1.0),
                    "span_mrr": min(base_mrr + 0.03, 1.0),
                    "span_hit_rate@1": min(base_mrr, 1.0),
                    "span_hit_rate@3": min(base_mrr + 0.12, 1.0),
                    "span_hit_rate@5": min(base_mrr + 0.17, 1.0),
                },
                "by_project": {
                    project: {
                        "mrr": base_mrr + offset * 0.001,
                        "hit_rate@1": base_mrr,
                        "hit_rate@3": min(base_mrr + 0.1, 1.0),
                        "hit_rate@5": min(base_mrr + 0.15, 1.0),
                        "span_mrr": min(base_mrr + 0.03, 1.0),
                        "span_hit_rate@5": min(base_mrr + 0.17, 1.0),
                    }
                    for offset, project in enumerate(retrieval_projects)
                },
            }
            boundary[method] = {
                "overall_micro": {
                    "precision": base_f1 - 0.01,
                    "recall": base_f1 + 0.01,
                    "f1": base_f1,
                    "f1_ci95": [base_f1 - 0.02, base_f1 + 0.02],
                },
                "by_project": {
                    project: {"f1": base_f1 + offset * 0.001}
                    for offset, project in enumerate(boundary_projects)
                },
            }
            efficiency[method] = {
                "parent_chunk_count": 1000 + method_index,
                "retrieval_vector_count": 2000 + method_index * 10,
                "chunking_seconds": 10.0 + method_index,
                "index_seconds": 20.0 + method_index,
                "query_latency_ms_mean": 5.0 + method_index,
                "query_latency_ms_p50": 4.5 + method_index,
                "query_latency_ms_p95": 8.0 + method_index,
            }
            if not is_primary:
                method_comparisons = []
                for name in ("mrr", "hit_rate@5", "span_mrr", "span_hit_rate@5"):
                    is_confirmatory = method == "llamaindex_sentence" and name == "mrr"
                    raw_p = 2 / 64 if significant else 16 / 64
                    primary_value = {
                        "mrr": 0.72,
                        "hit_rate@5": 0.87,
                        "span_mrr": 0.75,
                        "span_hit_rate@5": 0.89,
                    }[name]
                    comparison_value = retrieval[method]["overall_micro"][name]
                    mean_difference = primary_value - comparison_value
                    method_comparisons.append(
                        {
                            "metric": name,
                            "mean_difference": mean_difference,
                            "ci95": [mean_difference - 0.04, mean_difference + 0.04],
                            "p_value": raw_p,
                            "p_value_holm_secondary": (
                                None if is_confirmatory else min(1.0, raw_p * 55)
                            ),
                            "confirmatory_test": is_confirmatory,
                            "cluster_count": 6,
                            "randomization_unit": "project_id",
                            "randomization_mode": "exact_cluster_enumeration",
                            "randomization_permutations": 64,
                        }
                    )
                comparisons[method] = method_comparisons
                boundary_difference = 0.80 - base_f1
                boundary_raw_p = 2 / 64 if significant else 16 / 64
                boundary_comparisons[method] = {
                    "f1_difference": boundary_difference,
                    "ci95": [boundary_difference - 0.04, boundary_difference + 0.04],
                    "p_value": boundary_raw_p,
                    "p_value_holm": min(1.0, boundary_raw_p * 14),
                    "cluster_count": 6,
                    "randomization_unit": "project_id",
                    "randomization_mode": "exact_cluster_enumeration",
                    "randomization_permutations": 64,
                }
        return {
            "run_validation": [
                {
                    "project_id": item["project_id"],
                    "valid_formal_run": True,
                    "issues": [],
                }
                for item in manifest["projects"]
            ],
            "protocol_version": "industrial-seac-formal-analysis-v1.1",
            "primary_method": "proposed_v6",
            "confirmatory_comparator": "llamaindex_sentence",
            "confirmatory_metric": "mrr",
            "bootstrap_samples": 10000,
            "random_seed": 20260804,
            "randomization_unit": "project_id",
            "randomization_rule": "exact_cluster_enumeration_when_project_count_at_most_20",
            "retrieval_holm_family_size": 55,
            "boundary_holm_family_size": 14,
            "analysis_scope": "locked_test",
            "project_ids": projects,
            "project_count": len(projects),
            "retrieval": retrieval,
            "paired_retrieval_comparisons": comparisons,
            "boundary": boundary,
            "paired_boundary_comparisons": boundary_comparisons,
            "efficiency": efficiency,
        }

    def annotation(self) -> dict:
        return {
            "boundary_verification": {
                "kappa_threshold": 0.70,
                "human_verified_count": 783,
                "llm_prelabel_count": 1230,
                "rule_determined_count": 387,
                "withheld_projects": ["P08"],
                "projects": {
                    project: {
                        "cohen_kappa": 0.82,
                        # One project moves the other way, so the shift summary has
                        # to name both directions rather than only the flattering one.
                        "cohen_kappa_before_rule_amendment": 0.90 if project == "P09" else 0.61,
                        "raw_agreement": 0.9,
                    }
                    for project in ("P04", "P05", "P06", "P07", "P08", "P09", "P10")
                },
            },
            "qa_prereview": {
                "record_count": 262,
                "verdicts": {"APPROVED": 71, "NEEDS_REWRITE": 124, "REJECTED": 67},
            },
            "qa_candidates": {
                "candidate_total": 227,
                "project_count": 9,
                "multi_evidence_records": 60,
                "min_per_project": 18,
            },
            "qa_approval": {
                "approved": 205,
                "rejected": 20,
                "rule_excluded": 10,
                "project_count": 9,
                "written_total": 195,
                "section_scanning_rejections": 16,
                "rule_excluded_projects": ["P03", "P04", "P05"],
                "withheld_projects": ["P10"],
                "counts": {"P10": 11},
                "minimums": {"P10": 15},
            },
            "p01_privacy": {
                "reviewed": 27,
                "no_risk": 23,
                "anonymized": 0,
                "cleared": 1,
                "rejected": 3,
                "retained": 24,
            },
        }

    def test_mapping_covers_all_144_manuscript_placeholders(self) -> None:
        analysis = self.analysis()
        injector.validate_analysis(analysis, self.manifest())
        replacements = injector.build_replacements(analysis, self.annotation())
        source = (ROOT / "manuscript/seac_industry_manuscript.md").read_text(encoding="utf-8")
        self.assertEqual(144, len(replacements))
        completed = injector.fill_manuscript(source, replacements)
        self.assertNotRegex(completed, r"\{\{FORMAL_")

    def test_narratives_use_axis_eligible_project_counts(self) -> None:
        replacements = injector.build_replacements(self.analysis())
        self.assertIn("6 boundary-eligible locked projects", replacements["FORMAL_RQ1_NARRATIVE"])
        self.assertIn("/6 retrieval-eligible locked projects", replacements["FORMAL_RQ2_RQ3_NARRATIVE"])

    def test_qa_approval_placeholders_state_both_sides_of_the_rule(self) -> None:
        replacements = injector.annotation_replacements(self.annotation())
        self.assertEqual(
            "205 of 225 candidates across 9 projects",
            replacements["FORMAL_QA_APPROVAL_SUMMARY"],
        )
        self.assertIn("16", replacements["FORMAL_QA_REJECTION_SUMMARY"])
        rule = replacements["FORMAL_QA_RULE_EXCLUDED_SUMMARY"]
        self.assertIn("10 approved questions", rule)
        self.assertIn("P03, P04, P05", rule)
        self.assertIn("195", rule)

    def test_the_withheld_project_sentence_names_the_shortfall(self) -> None:
        """A reader checking the exclusion needs the count and the minimum."""

        replacements = injector.annotation_replacements(self.annotation())
        self.assertEqual(
            "P10 retained 11 of the 15 required questions.",
            replacements["FORMAL_QA_WITHHELD_SUMMARY"],
        )

    def test_the_p01_privacy_summary_accounts_for_every_row(self) -> None:
        """The verdict counts must add up to the rows reviewed, and state the loss."""

        replacements = injector.annotation_replacements(self.annotation())
        summary = replacements["FORMAL_P01_PRIVACY_SUMMARY"]
        self.assertIn("27 P01 questions", summary)
        self.assertIn("23 carried no identifiable information", summary)
        self.assertIn("1 was cleared", summary)
        self.assertIn("3 were rejected", summary)
        self.assertIn("leaving 24 questions", summary)

    def test_the_p01_privacy_counts_come_from_the_reviewed_file(self) -> None:
        """Counting must read the file the finalization gate consumes."""

        directory = Path(tempfile.mkdtemp())
        try:
            path = directory / "qa_reviewed.json"
            path.write_text(
                json.dumps(
                    [
                        {"qa_id": "QA001", "privacy_review_status": "NO_RISK"},
                        {"qa_id": "QA002", "privacy_review_status": "ANONYMIZED"},
                        {"qa_id": "QA003", "privacy_review_status": "cleared"},
                        {"qa_id": "QA004", "privacy_review_status": "REJECTED"},
                    ]
                ),
                encoding="utf-8",
            )
            outcome = injector.p01_privacy_outcome(path)
            self.assertEqual(4, outcome["reviewed"])
            self.assertEqual(1, outcome["anonymized"])
            # A lowercase verdict is the same verdict; it must not fall through.
            self.assertEqual(1, outcome["cleared"])
            self.assertEqual(3, outcome["retained"])
        finally:
            shutil.rmtree(directory, ignore_errors=True)

    def test_no_withheld_project_is_stated_plainly(self) -> None:
        annotation = self.annotation()
        annotation["qa_approval"]["withheld_projects"] = []
        replacements = injector.annotation_replacements(annotation)
        self.assertEqual(
            "No project fell below the minimum.", replacements["FORMAL_QA_WITHHELD_SUMMARY"]
        )

    def test_annotation_placeholders_report_mixed_reference_shares(self) -> None:
        replacements = injector.annotation_replacements(self.annotation())
        self.assertEqual("783 of 2400 (32.6%)", replacements["FORMAL_HUMAN_VERIFIED_SHARE"])
        self.assertEqual("1230 of 2400 (51.2%)", replacements["FORMAL_LLM_PRELABEL_SHARE"])
        self.assertEqual("387 of 2400 (16.1%)", replacements["FORMAL_RULE_DETERMINED_SHARE"])
        self.assertIn("kappa 0.82", replacements["FORMAL_PRELABEL_AGREEMENT_SUMMARY"])
        self.assertIn("71 of 262", replacements["FORMAL_QA_PREREVIEW_SUMMARY"])
        self.assertIn("227 questions across 9 projects", replacements["FORMAL_QA_CANDIDATE_SUMMARY"])
        self.assertIn("60 of them carrying an evidence equivalence set", replacements["FORMAL_QA_CANDIDATE_SUMMARY"])

    def test_withheld_project_is_named_and_kappa_shift_states_both_directions(self) -> None:
        """§4.1bis.5 — the disclosure must not report only the flattering direction."""

        replacements = injector.annotation_replacements(self.annotation())
        self.assertEqual("P08", replacements["FORMAL_BOUNDARY_WITHHELD_PROJECTS"])
        shift = replacements["FORMAL_PRELABEL_KAPPA_SHIFT_SUMMARY"]
        self.assertIn("raising it for P04, P05, P06, P07, P08, P10", shift)
        self.assertIn("lowering it for P09", shift)

    def test_boundary_verification_withholding_matches_frozen_manifest(self) -> None:
        verification = {
            "promotable": True,
            "withheld_projects": ["P08"],
            "blocked_projects": ["P08"],
        }
        injector.validate_boundary_verification(verification, self.manifest())

    def test_stale_boundary_verification_summary_is_rejected(self) -> None:
        verification = {
            "promotable": False,
            "withheld_projects": [],
            "blocked_projects": ["P08"],
        }
        with self.assertRaisesRegex(ValueError, "frozen manifest"):
            injector.validate_boundary_verification(verification, self.manifest())

    def test_passing_project_cannot_be_declared_withheld(self) -> None:
        verification = {
            "promotable": True,
            "withheld_projects": ["P08"],
            "blocked_projects": [],
        }
        with self.assertRaisesRegex(ValueError, "blockers must match"):
            injector.validate_boundary_verification(verification, self.manifest())

    def test_agreement_summary_reports_kappa_before_and_after(self) -> None:
        summary = injector.annotation_replacements(self.annotation())["FORMAL_PRELABEL_AGREEMENT_SUMMARY"]
        self.assertIn("P04 kappa 0.82 after the amendment and 0.61 before it", summary)
        self.assertIn("P09 kappa 0.82 after the amendment and 0.90 before it", summary)

    def test_nonlocked_analysis_is_rejected(self) -> None:
        analysis = self.analysis()
        analysis["analysis_scope"] = "all"
        with self.assertRaisesRegex(ValueError, "locked_test"):
            injector.validate_analysis(analysis, self.manifest())

    def test_question_level_randomization_is_rejected(self) -> None:
        analysis = self.analysis()
        analysis["paired_retrieval_comparisons"]["fixed"][0]["randomization_unit"] = "qa_id"
        with self.assertRaisesRegex(ValueError, "project level"):
            injector.validate_analysis(analysis, self.manifest())

    def test_nonsignificant_gain_uses_cautious_language(self) -> None:
        replacements = injector.build_replacements(self.analysis(significant=False))
        self.assertIn("not a statistically significant confirmatory advantage", replacements["FORMAL_CONCLUSION_CLAUSE"])

    def test_off_grid_exact_p_value_is_rejected(self) -> None:
        analysis = self.analysis()
        analysis["paired_retrieval_comparisons"]["fixed"][0]["p_value"] = 0.2
        with self.assertRaisesRegex(ValueError, "enumeration grid"):
            injector.validate_analysis(analysis, self.manifest())

    def test_wrong_confirmatory_comparator_is_rejected(self) -> None:
        analysis = self.analysis()
        analysis["confirmatory_comparator"] = "fixed"
        with self.assertRaisesRegex(ValueError, "Confirmatory comparator"):
            injector.validate_analysis(analysis, self.manifest())

    def test_mismatched_paired_effect_is_rejected(self) -> None:
        analysis = self.analysis()
        analysis["paired_retrieval_comparisons"]["fixed"][0]["mean_difference"] = 0.0
        with self.assertRaisesRegex(ValueError, "disagrees with the micro summary"):
            injector.validate_analysis(analysis, self.manifest())

    def test_jss_highlights_meet_count_and_length_limits(self) -> None:
        highlights = injector.build_highlights(self.analysis())
        self.assertGreaterEqual(len(highlights), 3)
        self.assertLessEqual(len(highlights), 5)
        self.assertTrue(all(len(item) <= 85 for item in highlights))


if __name__ == "__main__":
    unittest.main()
