import importlib.util
import tempfile
import unittest
from pathlib import Path


SCRIPT_PATH = Path(__file__).parents[1] / "scripts" / "convert_legacy_sources.py"
SPEC = importlib.util.spec_from_file_location("convert_legacy_sources", SCRIPT_PATH)
conversion = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(conversion)


class SourceConversionTests(unittest.TestCase):
    def test_container_kind_detects_ole_and_zip(self):
        with tempfile.TemporaryDirectory() as temp_name:
            temp = Path(temp_name)
            ole = temp / "legacy.docx"
            archive = temp / "valid.docx"
            ole.write_bytes(conversion.OLE_MAGIC + b"rest")
            archive.write_bytes(b"PK\x03\x04rest")
            self.assertEqual("ole", conversion.container_kind(ole))
            self.assertEqual("zip", conversion.container_kind(archive))

    def test_output_relative_path_normalizes_doc_suffix(self):
        self.assertEqual(
            Path("meetings/example.docx"),
            conversion.output_relative_path(Path("meetings/example.doc")),
        )


if __name__ == "__main__":
    unittest.main()
