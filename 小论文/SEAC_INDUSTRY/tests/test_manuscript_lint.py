import importlib.util
import sys
import unittest
from pathlib import Path


SCRIPT_PATH = Path(__file__).parents[1] / "scripts" / "lint_manuscript.py"
sys.path.insert(0, str(SCRIPT_PATH.parent))
SPEC = importlib.util.spec_from_file_location("lint_manuscript", SCRIPT_PATH)
lint_module = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(lint_module)


class ManuscriptLintTests(unittest.TestCase):
    def manuscript(self, body: str = "[1]", references: str = "[1] ref") -> str:
        sections = [
            section
            for section in lint_module.REQUIRED_SECTIONS
            if section != "## References"
        ]
        return "\n".join(sections) + f"\n{body}\n## References\n\n{references}"

    def test_draft_placeholder_is_warning_but_final_error(self):
        base = self.manuscript(body="[1]\n{{FORMAL_VALUE}}")
        draft = lint_module.lint(base)
        final = lint_module.lint(base, require_final=True)
        self.assertTrue(draft["clean"])
        self.assertFalse(final["clean"])

    def test_sensitive_project_name_is_error(self):
        base = self.manuscript(body=f"[1]\n{lint_module.BANNED_IDENTIFIERS[0]}")
        result = lint_module.lint(base)
        self.assertFalse(result["clean"])

    def test_uncited_reference_is_error(self):
        result = lint_module.lint(self.manuscript(references="[1] ref\n[2] uncited"))
        self.assertFalse(result["clean"])
        self.assertIn(
            "reference entries are not cited in the manuscript body: [2]",
            result["errors"],
        )

    def test_missing_reference_is_error(self):
        result = lint_module.lint(self.manuscript(body="[1], [2]"))
        self.assertFalse(result["clean"])
        self.assertIn("citations lack reference entries: [2]", result["errors"])


if __name__ == "__main__":
    unittest.main()
