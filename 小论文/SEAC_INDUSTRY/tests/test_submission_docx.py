from __future__ import annotations

import importlib.util
import re
import sys
import tempfile
import unittest
from pathlib import Path

from docx import Document


ROOT = Path(__file__).parents[1]
SCRIPT_PATH = ROOT / "scripts" / "build_submission_docx.py"
sys.path.insert(0, str(SCRIPT_PATH.parent))
SPEC = importlib.util.spec_from_file_location("build_submission_docx", SCRIPT_PATH)
builder = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(builder)


class SubmissionDocxTests(unittest.TestCase):
    def test_latex_fraction_is_converted_to_editable_linear_math(self) -> None:
        formula, tag = builder.simplify_latex(r"x=\frac{a}{b}\tag{3}")
        self.assertEqual("3", tag)
        self.assertEqual("x=(a)/(b)", formula)

    def test_markdown_builds_editable_table_and_equation(self) -> None:
        source = r"""# Test title

## Abstract

Test abstract.

\[
x=1
\\tag{1}
\]

**Table 1. Test table.**

| A | B |
|---|---:|
| one | 1 |

## References

Author, 2026. Test.
"""
        document = builder.markdown_to_docx(source)
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "test.docx"
            document.save(path)
            loaded = Document(path)
            self.assertGreaterEqual(len(loaded.tables), 2)
            self.assertIn("Test title", "\n".join(item.text for item in loaded.paragraphs))

    def test_final_validation_rejects_anonymous_layout_draft(self) -> None:
        with self.assertRaisesRegex(ValueError, "layout draft"):
            builder.validate_submission_text("Anonymous layout draft", allow_layout_draft=False)

    def test_separate_numbered_lists_preserve_source_numbers(self) -> None:
        source = "1. First\n2. Second\n\nBetween.\n\n1. Reset\n2. Again\n"
        document = builder.markdown_to_docx(source)
        numbered = [paragraph.text for paragraph in document.paragraphs if re.match(r"^\d+\. ", paragraph.text)]
        self.assertEqual(["1. First", "2. Second", "1. Reset", "2. Again"], numbered)


if __name__ == "__main__":
    unittest.main()
