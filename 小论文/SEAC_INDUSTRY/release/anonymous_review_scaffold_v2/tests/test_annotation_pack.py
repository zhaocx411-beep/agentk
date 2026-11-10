import importlib.util
import sys
import unittest
from pathlib import Path

from seac_chunking.models import Unit


SCRIPT_PATH = Path(__file__).parents[1] / "scripts" / "prepare_annotation_pack.py"
sys.path.insert(0, str(SCRIPT_PATH.parent))
SPEC = importlib.util.spec_from_file_location("prepare_annotation_pack", SCRIPT_PATH)
annotation = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(annotation)


def unit(unit_id, text, block_type="paragraph", heading="", group=""):
    return Unit(unit_id, unit_id.split("#")[0], "doc", "SRS", block_type, heading, text, 1, group)


class AnnotationPackTests(unittest.TestCase):
    def test_boundary_strata(self):
        self.assertEqual(
            "inside_table",
            annotation.boundary_stratum(
                unit("T1#R001", "表头", "table_row", group="T1"),
                unit("T1#R002", "数据", "table_row", group="T1"),
            ),
        )
        self.assertEqual(
            "heading_transition",
            annotation.boundary_stratum(unit("B1", "正文", heading="A"), unit("B2", "标题", "heading", "B")),
        )

    def test_sampling_is_deterministic_and_blind(self):
        pool = [
            {
                "sampling_identity": f"id-{index}",
                "project_id": "PX",
                "source_document": f"doc-{index % 2}",
                "boundary_stratum": "same_section_continuity",
                "human_label": "",
            }
            for index in range(20)
        ]
        first = annotation.stratified_sample([dict(item) for item in pool], 8, 7)
        second = annotation.stratified_sample([dict(item) for item in pool], 8, 7)
        self.assertEqual(first, second)
        self.assertTrue(all("suggested_label" not in item for item in first))

    def test_readme_points_reviewers_to_normative_protocol(self):
        readme = annotation.annotation_readme("PX", 300, 36)
        self.assertIn("docs/annotation_and_privacy_protocol.md", readme)
        self.assertIn("privacy_review_status", readme)


if __name__ == "__main__":
    unittest.main()
