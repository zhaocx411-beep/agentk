from __future__ import annotations

import argparse
import os
import tempfile
import zipfile
from pathlib import Path
from xml.etree import ElementTree as ET


CORE_MEMBER = "docProps/core.xml"
NAMESPACES = {
    "dc": "http://purl.org/dc/elements/1.1/",
    "cp": "http://schemas.openxmlformats.org/package/2006/metadata/core-properties",
}


def scrub_core_properties(payload: bytes) -> bytes:
    root = ET.fromstring(payload)
    for prefix, local_name in (("dc", "creator"), ("cp", "lastModifiedBy")):
        element = root.find(f"{{{NAMESPACES[prefix]}}}{local_name}")
        if element is not None:
            element.text = ""
    return ET.tostring(root, encoding="utf-8", xml_declaration=True)


def main() -> None:
    parser = argparse.ArgumentParser(description="Clear identity-bearing DOCX core properties.")
    parser.add_argument("docx", type=Path)
    args = parser.parse_args()
    docx = args.docx.resolve()

    with zipfile.ZipFile(docx, "r") as source_zip:
        if CORE_MEMBER not in source_zip.namelist():
            raise KeyError(f"Missing DOCX core properties: {CORE_MEMBER}")
        fd, temporary_name = tempfile.mkstemp(prefix=f".{docx.stem}-", suffix=".docx", dir=docx.parent)
        os.close(fd)
        temporary = Path(temporary_name)
        try:
            with zipfile.ZipFile(temporary, "w") as target_zip:
                for info in source_zip.infolist():
                    payload = source_zip.read(info.filename)
                    if info.filename == CORE_MEMBER:
                        payload = scrub_core_properties(payload)
                    target_zip.writestr(info, payload)
            os.replace(temporary, docx)
        finally:
            temporary.unlink(missing_ok=True)


if __name__ == "__main__":
    main()
