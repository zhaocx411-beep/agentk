import importlib.util
import json
import tempfile
import unittest
from collections import Counter
from pathlib import Path


ROOT = Path(__file__).parents[1]


def load(name: str):
    path = ROOT / "scripts" / f"{name}.py"
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


prepare = load("prepare_llm_prelabel_batches")
merge = load("merge_llm_prelabels")
audit = load("audit_prelabel_consistency")
verification = load("build_prelabel_verification_sample")
promotion = load("promote_llm_prelabels")
qa_prereview = load("merge_qa_prereview")


def candidate(candidate_id: str, **overrides) -> dict:
    record = {
        "candidate_id": candidate_id,
        "project_id": "P02",
        "source_document": "doc.docx",
        "document_type": "Detailed Design",
        "boundary_stratum": "same_section_continuity",
        "left_section": "系统设计 > 模块",
        "left_block_type": "paragraph",
        "left_evidence": "左侧正文内容",
        "right_section": "系统设计 > 模块",
        "right_block_type": "paragraph",
        "right_evidence": "右侧正文内容",
    }
    record.update(overrides)
    return record


def prelabelled(candidate_id: str, label: str, flag: str = "CLEAR", **overrides) -> dict:
    return {
        **candidate(candidate_id, **overrides),
        "prelabel": label,
        "prelabel_flag": flag,
        "prelabel_notes": "依据",
    }


class BatchRenderTests(unittest.TestCase):
    def test_identical_sections_are_collapsed(self):
        rendered = prepare.render(candidate("P02-BP0001"))
        self.assertIn("sec=<same>", rendered)
        self.assertIn("[P02-BP0001]", rendered)

    def test_visible_fields_exclude_labels_and_predictions(self):
        self.assertNotIn("human_label", prepare.VISIBLE_FIELDS)
        self.assertNotIn("suggested_label", prepare.VISIBLE_FIELDS)


class LabelFileValidationTests(unittest.TestCase):
    def write(self, lines: list[str]) -> Path:
        directory = Path(tempfile.mkdtemp())
        path = directory / "P02_b01.jsonl"
        path.write_text("\n".join(lines) + "\n", encoding="utf-8")
        return path

    def test_valid_line_is_parsed(self):
        path = self.write(['{"id":"P02-BP0001","label":"break","flag":"clear","note":"依据"}'])
        records = merge.read_label_lines(path)
        self.assertEqual([{"id": "P02-BP0001", "label": "BREAK", "flag": "CLEAR", "note": "依据"}], records)

    def test_invalid_label_is_rejected(self):
        path = self.write(['{"id":"P02-BP0001","label":"MAYBE","flag":"CLEAR","note":"依据"}'])
        with self.assertRaisesRegex(ValueError, "invalid label"):
            merge.read_label_lines(path)

    def test_empty_note_is_rejected(self):
        path = self.write(['{"id":"P02-BP0001","label":"BREAK","flag":"CLEAR","note":"  "}'])
        with self.assertRaisesRegex(ValueError, "empty note"):
            merge.read_label_lines(path)

    def test_missing_field_is_rejected(self):
        path = self.write(['{"id":"P02-BP0001","label":"BREAK","note":"依据"}'])
        with self.assertRaisesRegex(ValueError, "missing field"):
            merge.read_label_lines(path)


class RuleConformanceTests(unittest.TestCase):
    def test_bare_template_label_must_be_noise(self):
        record = prelabelled("P02-BP0001", "NO_BREAK", left_evidence="输出")
        self.assertIn("bare_template_label_not_noise", audit.rule_checks(record))
        record["prelabel"] = "NOISE"
        self.assertEqual([], audit.rule_checks(record))

    def test_inside_table_break_is_reported(self):
        record = prelabelled("P02-BP0002", "BREAK", boundary_stratum="inside_table")
        self.assertIn("inside_table_marked_break", audit.rule_checks(record))

    def test_page_numbered_heading_must_be_noise(self):
        record = prelabelled(
            "P02-BP0003",
            "BREAK",
            left_block_type="heading",
            left_evidence="3.18.1 功能描述 336",
        )
        self.assertIn("page_numbered_heading_not_noise", audit.rule_checks(record))

    def test_parent_child_heading_break_is_reported(self):
        record = prelabelled(
            "P02-BP0004",
            "BREAK",
            left_block_type="heading",
            right_block_type="heading",
            left_section="系统设计 > 模块",
            right_section="系统设计 > 模块 > 画面设计",
            left_evidence="模块",
            right_evidence="画面设计",
        )
        self.assertIn("parent_child_heading_marked_break", audit.rule_checks(record))

    def test_conforming_record_has_no_finding(self):
        self.assertEqual([], audit.rule_checks(prelabelled("P02-BP0005", "NO_BREAK")))


class VerificationSampleTests(unittest.TestCase):
    def records(self) -> list[dict]:
        strata = ["inside_table"] * 60 + ["heading_transition"] * 30 + ["noise_candidate"] * 10
        return [
            prelabelled(f"P02-BP{index:04d}", "NO_BREAK", boundary_stratum=stratum)
            for index, stratum in enumerate(strata, 1)
        ]

    def test_sample_size_and_determinism(self):
        records = self.records()
        first = verification.stratified_sample(records, 20, "seed:P02")
        second = verification.stratified_sample(records, 20, "seed:P02")
        self.assertEqual(20, len(first))
        self.assertEqual([item["candidate_id"] for item in first], [item["candidate_id"] for item in second])

    def test_sample_is_stratum_proportional(self):
        counts = Counter(
            item["boundary_stratum"] for item in verification.stratified_sample(self.records(), 20, "seed:P02")
        )
        self.assertEqual({"inside_table": 12, "heading_transition": 6, "noise_candidate": 2}, dict(counts))

    def test_sample_ids_are_unique(self):
        sample = verification.stratified_sample(self.records(), 40, "seed:P02")
        self.assertEqual(40, len({item["candidate_id"] for item in sample}))


class PromotionGateTests(unittest.TestCase):
    def expected(self, sample_ids: list[str], flagged_ids: list[str]) -> dict:
        return {"agreement_sample_ids": sample_ids, "flagged_review_ids": flagged_ids}

    def verified_row(self, **overrides) -> dict:
        row = {"row_group": "AGREEMENT_SAMPLE", "human_label": "BREAK", "review_status": "COMPLETE", "review_notes": ""}
        row.update(overrides)
        return row

    def test_incomplete_row_blocks_promotion(self):
        records = [prelabelled("P02-BP0001", "BREAK")]
        verified = {"P02-BP0001": self.verified_row(human_label="", review_status="UNLABELED")}
        audit_result = promotion.audit_project("P02", records, verified, self.expected(["P02-BP0001"], []))
        self.assertTrue(any("valid label" in blocker for blocker in audit_result["blockers"]))

    def test_flagged_ambiguous_row_is_promotable_and_counted(self):
        """Protocol 4.4 asks for a best judgment plus a flag, so it must not deadlock the gate."""

        records = [prelabelled("P02-BP0001", "BREAK")]
        verified = {
            "P02-BP0001": self.verified_row(review_status="NEEDS_DISCUSSION", review_notes="标题与首段关系不明")
        }
        audit_result = promotion.audit_project("P02", records, verified, self.expected(["P02-BP0001"], []))
        self.assertEqual([], audit_result["blockers"])
        self.assertEqual(1, audit_result["needs_discussion_count"])
        self.assertEqual(["P02-BP0001"], audit_result["needs_discussion_ids"])

    def test_flagged_row_stays_in_the_agreement_statistic(self):
        records = [prelabelled("P02-BP0001", "NO_BREAK")]
        verified = {
            "P02-BP0001": self.verified_row(review_status="NEEDS_DISCUSSION", review_notes="歧义来源")
        }
        audit_result = promotion.audit_project("P02", records, verified, self.expected(["P02-BP0001"], []))
        self.assertEqual(1, audit_result["agreement_sample_size"])
        self.assertEqual(1, audit_result["disagreement_count"])

    def test_flagged_row_without_a_reason_blocks_promotion(self):
        records = [prelabelled("P02-BP0001", "BREAK")]
        verified = {"P02-BP0001": self.verified_row(review_status="NEEDS_DISCUSSION")}
        audit_result = promotion.audit_project("P02", records, verified, self.expected(["P02-BP0001"], []))
        self.assertTrue(any("no reason" in blocker for blocker in audit_result["blockers"]))

    def test_rule_determined_rows_leave_the_agreement_statistic(self):
        """§4.1bis.6 — a row neither side judged says nothing about agreement."""

        records = [
            prelabelled("P02-BP0001", "BREAK", left_evidence="}", right_evidence="取餐柜管理箱门接口"),
            prelabelled("P02-BP0002", "NO_BREAK", left_evidence="押金退还流程如下所述。", right_evidence="退宿当日生成结算单。"),
        ]
        verified = {
            "P02-BP0001": self.verified_row(human_label="NOISE"),
            "P02-BP0002": self.verified_row(human_label="NO_BREAK"),
        }
        audit_result = promotion.audit_project(
            "P02", records, verified, self.expected(["P02-BP0001", "P02-BP0002"], [])
        )
        self.assertEqual(2, audit_result["verification_sample_size"])
        self.assertEqual(1, audit_result["rule_determined_count"])
        self.assertEqual(1, audit_result["agreement_sample_size"])
        self.assertEqual({"LEFT_CONVERSION_FRAGMENT": 1}, audit_result["rule_determined_reasons"])

    def test_both_kappas_are_reported_for_disclosure(self):
        """§4.1bis.5 — the pre-amendment figure must stay visible."""

        records = [
            prelabelled(f"P02-BP{index:04d}", "BREAK", left_evidence="}", right_evidence="新接口名称")
            for index in range(1, 5)
        ]
        verified = {record["candidate_id"]: self.verified_row(human_label="NOISE") for record in records}
        audit_result = promotion.audit_project(
            "P02", records, verified, self.expected([record["candidate_id"] for record in records], [])
        )
        self.assertIn("cohen_kappa_before_rule_amendment", audit_result)
        self.assertEqual(0, audit_result["agreement_sample_size"])
        self.assertEqual(4, audit_result["rule_determined_count"])

    def test_missing_row_blocks_promotion(self):
        records = [prelabelled("P02-BP0001", "BREAK")]
        audit_result = promotion.audit_project("P02", records, {}, self.expected(["P02-BP0001"], []))
        self.assertTrue(any("missing" in blocker for blocker in audit_result["blockers"]))

    def test_agreement_and_kappa_are_measured_on_sample_only(self):
        records = [prelabelled(f"P02-BP{index:04d}", "BREAK" if index % 2 else "NO_BREAK") for index in range(1, 5)]
        verified = {
            "P02-BP0001": {"row_group": "AGREEMENT_SAMPLE", "human_label": "BREAK", "review_status": "COMPLETE", "review_notes": ""},
            "P02-BP0002": {"row_group": "AGREEMENT_SAMPLE", "human_label": "NO_BREAK", "review_status": "COMPLETE", "review_notes": ""},
            "P02-BP0003": {"row_group": "FLAGGED_REVIEW", "human_label": "NOISE", "review_status": "COMPLETE", "review_notes": ""},
        }
        audit_result = promotion.audit_project(
            "P02", records, verified, self.expected(["P02-BP0001", "P02-BP0002"], ["P02-BP0003"])
        )
        self.assertEqual([], audit_result["blockers"])
        self.assertEqual(2, audit_result["agreement_sample_size"])
        self.assertEqual(1.0, audit_result["raw_agreement"])
        self.assertEqual(1, audit_result["flagged_corrected_count"])

    def test_disagreement_lowers_kappa(self):
        records = [prelabelled(f"P02-BP{index:04d}", "BREAK") for index in range(1, 5)]
        verified = {
            f"P02-BP{index:04d}": {
                "row_group": "AGREEMENT_SAMPLE",
                "human_label": "BREAK" if index == 1 else "NO_BREAK",
                "review_status": "COMPLETE",
                "review_notes": "",
            }
            for index in range(1, 5)
        }
        audit_result = promotion.audit_project(
            "P02", records, verified, self.expected([f"P02-BP{index:04d}" for index in range(1, 5)], [])
        )
        self.assertEqual(0.25, audit_result["raw_agreement"])
        self.assertLess(audit_result["cohen_kappa"], 0.70)


class QaPrereviewValidationTests(unittest.TestCase):
    def write(self, payload: dict) -> Path:
        directory = Path(tempfile.mkdtemp())
        path = directory / "P02.jsonl"
        path.write_text(json.dumps(payload, ensure_ascii=False) + "\n", encoding="utf-8")
        return path

    def valid(self) -> dict:
        return {
            "id": "P02-QA001",
            "content_verdict": "APPROVED",
            "difficulty": "Easy",
            "suggested_privacy_flags": ["PERSON_NAME_OR_CONTACT"],
            "note": "证据唯一",
        }

    def test_valid_record_is_accepted(self):
        self.assertEqual(1, len(qa_prereview.read_verdicts(self.write(self.valid()))))

    def test_unknown_privacy_flag_is_rejected(self):
        payload = {**self.valid(), "suggested_privacy_flags": ["UNKNOWN_CATEGORY"]}
        with self.assertRaisesRegex(ValueError, "unknown privacy flags"):
            qa_prereview.read_verdicts(self.write(payload))

    def test_invalid_verdict_is_rejected(self):
        payload = {**self.valid(), "content_verdict": "MAYBE"}
        with self.assertRaisesRegex(ValueError, "invalid verdict"):
            qa_prereview.read_verdicts(self.write(payload))

    def test_invalid_difficulty_is_rejected(self):
        payload = {**self.valid(), "difficulty": "trivial"}
        with self.assertRaisesRegex(ValueError, "invalid difficulty"):
            qa_prereview.read_verdicts(self.write(payload))


if __name__ == "__main__":
    unittest.main()
