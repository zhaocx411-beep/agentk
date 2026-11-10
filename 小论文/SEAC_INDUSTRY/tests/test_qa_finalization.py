import importlib.util
import sys
import unittest
from pathlib import Path


SCRIPT_PATH = Path(__file__).parents[1] / "scripts" / "finalize_qa_review.py"
sys.path.insert(0, str(SCRIPT_PATH.parents[1]))
SPEC = importlib.util.spec_from_file_location("finalize_qa_review", SCRIPT_PATH)
finalization = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(finalization)


class QaFinalizationTests(unittest.TestCase):
    def test_approved_question_requires_exact_unit_and_privacy_clearance(self):
        documents = [
            {
                "file_name": "doc",
                "document_type": "SRS",
                "blocks": [
                    {
                        "block_id": "B1",
                        "block_type": "paragraph",
                        "heading_path": "",
                        "text": "接口请求方式为 POST",
                    }
                ],
            }
        ]
        questions = [
            {
                "qa_id": "Q1",
                "question": "请求方式是什么？",
                "reference_answer": "POST",
                "gold_evidence_id": "B1",
                "gold_evidence_unit_id": "B1",
                "gold_evidence_span": "请求方式为 POST",
                "approval_status": "APPROVED",
                "privacy_review_status": "CLEARED",
            }
        ]
        approved, summary = finalization.audit_questions(documents, questions)
        self.assertEqual(1, len(approved))
        self.assertTrue(summary["formal_ready"])

    def test_pending_draft_is_not_formal_ready(self):
        approved, summary = finalization.audit_questions([], [{"qa_id": "Q1", "approval_status": "PENDING"}])
        self.assertFalse(approved)
        self.assertFalse(summary["formal_ready"])


if __name__ == "__main__":
    unittest.main()
