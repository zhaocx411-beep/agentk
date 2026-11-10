from __future__ import annotations

import unittest

from scripts.import_review_workbooks import (
    BOUNDARY_HEADERS,
    QA_HEADERS,
    validate_boundary_rows,
    validate_qa_rows,
)


class WorkbookImportTest(unittest.TestCase):
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
        return {
            "qa_id": "QA001",
            "project_id": "P02",
            "question": "Question?",
            "reference_answer": "Answer",
            "source_document": "doc.docx",
            "source_section": "Section",
            "question_type": "Feature",
            "difficulty": "medium",
            "gold_evidence_id": "doc:B0001",
            "gold_evidence_unit_id": "doc:B0001",
            "gold_evidence_span": "Answer",
            "evidence_alignment_status": "BLOCK_IS_UNIT",
            "privacy_flags": [],
            "approval_status": "PENDING",
            "privacy_review_status": "PENDING",
            "review_notes": "",
            "privacy_review_notes": "",
        }

    def test_boundary_complete_import(self) -> None:
        source = self.boundary_source()
        row = [source.get(field, "") for field in BOUNDARY_HEADERS]
        row[11:14] = ["BREAK", "COMPLETE", "clear transition"]
        reviewed = validate_boundary_rows([BOUNDARY_HEADERS, row], [source], "P02", "A")
        self.assertEqual(reviewed[0]["human_label"], "BREAK")

    def test_boundary_rejects_immutable_edit(self) -> None:
        source = self.boundary_source()
        row = [source.get(field, "") for field in BOUNDARY_HEADERS]
        row[2] = "different.docx"
        row[11:14] = ["BREAK", "COMPLETE", ""]
        with self.assertRaisesRegex(ValueError, "immutable field source_document"):
            validate_boundary_rows([BOUNDARY_HEADERS, row], [source], "P02", "A")

    def test_boundary_rejects_incomplete_row(self) -> None:
        source = self.boundary_source()
        row = [source.get(field, "") for field in BOUNDARY_HEADERS]
        with self.assertRaisesRegex(ValueError, "human_label"):
            validate_boundary_rows([BOUNDARY_HEADERS, row], [source], "P02", "A")

    def test_boundary_rejects_wrong_column_count(self) -> None:
        source = self.boundary_source()
        row = [source.get(field, "") for field in BOUNDARY_HEADERS][:-1]
        with self.assertRaisesRegex(ValueError, "has 13 columns; expected 14"):
            validate_boundary_rows([BOUNDARY_HEADERS, row], [source], "P02", "A")

    def test_qa_approved_anonymized_import(self) -> None:
        source = self.qa_source()
        row = [source.get(field, "") for field in QA_HEADERS]
        row[12] = "CUSTOMER_OR_VENDOR_IDENTITY"
        row[13] = "APPROVED"
        row[14] = "ANONYMIZED"
        row[16] = "Replaced the company name with [CUSTOMER]."
        reviewed = validate_qa_rows([QA_HEADERS, row], [source], "P02")
        self.assertEqual(reviewed[0]["privacy_flags"], ["CUSTOMER_OR_VENDOR_IDENTITY"])

    def test_qa_rejects_approved_pending_privacy(self) -> None:
        source = self.qa_source()
        row = [source.get(field, "") for field in QA_HEADERS]
        row[13] = "APPROVED"
        with self.assertRaisesRegex(ValueError, "lacks privacy clearance"):
            validate_qa_rows([QA_HEADERS, row], [source], "P02")


if __name__ == "__main__":
    unittest.main()
