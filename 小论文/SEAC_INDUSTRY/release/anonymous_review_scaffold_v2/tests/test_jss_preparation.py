from __future__ import annotations

import importlib.util
import sys
import unittest
from pathlib import Path


ROOT = Path(__file__).parents[1]
SCRIPT_PATH = ROOT / "scripts" / "prepare_jss_manuscript.py"
sys.path.insert(0, str(SCRIPT_PATH.parent))
SPEC = importlib.util.spec_from_file_location("prepare_jss_manuscript", SCRIPT_PATH)
jss = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(jss)


class JssPreparationTests(unittest.TestCase):
    def completed_source(self) -> str:
        source = (ROOT / "manuscript/seac_industry_manuscript.md").read_text(encoding="utf-8")
        return source.replace("{{", "FORMAL_OPEN_").replace("}}", "_FORMAL_CLOSE")

    def test_anonymous_layout_conversion_uses_author_year_references(self) -> None:
        output = jss.prepare(self.completed_source(), anonymous_draft=True)
        self.assertIn("(Lewis et al., 2020)", output)
        self.assertNotIn("[1]", output)
        self.assertNotRegex(output, r"^\[\d+\] ")
        self.assertLessEqual(jss.abstract_word_count(output), 250)

    def test_verified_references_use_complete_stable_records(self) -> None:
        references = "\n".join(jss.JSS_REFERENCES)
        self.assertEqual(23, len(jss.CITATIONS))
        self.assertEqual(23, len(jss.JSS_REFERENCES))
        self.assertNotIn("Auer, C., et al.", references)
        self.assertIn("Staar, P.W.J.", references)
        self.assertIn("10.18653/v1/2024.findings-acl.137", references)
        self.assertIn("10.18653/v1/2024.emnlp-main.845", references)
        self.assertIn("langchain-text-splitters/1.1.2", references)
        self.assertIn("llama-index-core/0.14.23", references)
        self.assertIn("10.18653/v1/2025.findings-acl.422", references)
        self.assertIn("10.18653/v1/2025.acl-long.258", references)
        self.assertIn("10.1007/s00766-023-00412-z", references)

    def test_final_conversion_requires_author_metadata(self) -> None:
        with self.assertRaisesRegex(ValueError, "author metadata"):
            jss.prepare(self.completed_source())

    def test_author_metadata_is_validated(self) -> None:
        metadata = {
            "authors": [{"name": "A. Author", "affiliation_ids": ["1"], "orcid": ""}],
            "affiliations": [{"id": "1", "name": "University, City, Country"}],
            "corresponding_author": {"name": "A. Author", "email": "a@example.edu"},
        }
        output = jss.prepare(self.completed_source(), metadata=metadata)
        self.assertIn("A. Author", output)
        # Match the marker itself: the word alone also occurs in the annotation
        # section, where a project's withheld boundary standard is discussed.
        self.assertNotIn("Author information withheld", output)


if __name__ == "__main__":
    unittest.main()
