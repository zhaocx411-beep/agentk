import importlib.util
import sys
import unittest
from pathlib import Path


SCRIPT_PATH = Path(__file__).parents[1] / "scripts" / "prepare_qa_privacy_review.py"
sys.path.insert(0, str(SCRIPT_PATH.parent))
SPEC = importlib.util.spec_from_file_location("prepare_qa_privacy_review", SCRIPT_PATH)
privacy = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(privacy)


class PrivacyReviewTests(unittest.TestCase):
    def test_prescreen_flags_but_never_clears(self):
        records, summary = privacy.prepare(
            [
                {
                    "qa_id": "Q1",
                    "question": "接口地址是什么？",
                    "reference_answer": "https://internal.example/api?accessToken={token}",
                    "source_document": "客户甲接口.docx",
                }
            ],
            ["客户甲"],
        )
        self.assertEqual("PENDING", records[0]["privacy_review_status"])
        self.assertIn("INTERNAL_NETWORK_OR_URL", records[0]["privacy_flags"])
        self.assertIn("SECRET_OR_CREDENTIAL", records[0]["privacy_flags"])
        self.assertIn("CUSTOMER_OR_VENDOR_IDENTITY", records[0]["privacy_flags"])
        self.assertEqual("HUMAN_REVIEW_PENDING", summary["privacy_status"])


if __name__ == "__main__":
    unittest.main()
