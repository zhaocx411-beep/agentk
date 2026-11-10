from __future__ import annotations

import unittest

from scripts.audit_review_progress import boundary_row_state, qa_row_state
from scripts.import_review_workbooks import BOUNDARY_HEADERS, QA_HEADERS


class ReviewProgressTest(unittest.TestCase):
    def boundary_source(self) -> dict:
        return {
            "candidate_id": "P02-BP0001",
            "project_id": "P02",
            "source_document": "doc.docx",
            "document_type": "SRS",
            "boundary_stratum": "heading_transition",
            "left_section": "A",
            "left_block_type": "paragraph",
            "left_evidence": "left",
            "right_section": "B",
            "right_block_type": "heading",
            "right_evidence": "right",
            "human_label": "",
            "review_status": "UNLABELED",
            "review_notes": "",
        }

    def qa_source(self) -> dict:
        return dict(
            zip(
                QA_HEADERS,
                [
                    "QA001",
                    "P02",
                    "Question?",
                    "Answer",
                    "doc.docx",
                    "Section",
                    "Feature",
                    "medium",
                    "doc:B0001",
                    "doc:B0001",
                    "Answer",
                    "BLOCK_IS_UNIT",
                    "",
                    "PENDING",
                    "PENDING",
                    "",
                    "",
                ],
            )
        )

    def test_boundary_pending_then_complete(self) -> None:
        source = self.boundary_source()
        row = {field: source.get(field, "") for field in BOUNDARY_HEADERS}
        self.assertEqual(boundary_row_state(row, source, "P02", "A")[0], "pending")
        row.update(human_label="BREAK", review_status="COMPLETE")
        self.assertEqual(boundary_row_state(row, source, "P02", "A")[0], "complete")

    def test_boundary_immutable_edit_is_invalid(self) -> None:
        source = self.boundary_source()
        row = {field: source.get(field, "") for field in BOUNDARY_HEADERS}
        row["left_evidence"] = "changed"
        self.assertEqual(boundary_row_state(row, source, "P02", "A")[0], "invalid")

    def test_qa_pending_then_complete(self) -> None:
        source = self.qa_source()
        row = dict(source)
        self.assertEqual(qa_row_state(row, source, "P02")[0], "pending")
        row.update(approval_status="APPROVED", privacy_review_status="NO_RISK")
        self.assertEqual(qa_row_state(row, source, "P02")[0], "complete")

    def test_qa_decision_without_privacy_is_partial(self) -> None:
        source = self.qa_source()
        row = dict(source, approval_status="APPROVED")
        self.assertEqual(qa_row_state(row, source, "P02")[0], "partial")


if __name__ == "__main__":
    unittest.main()
