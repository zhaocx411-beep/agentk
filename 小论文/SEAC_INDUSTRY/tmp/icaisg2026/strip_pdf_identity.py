from __future__ import annotations

import argparse
import os
import tempfile
from pathlib import Path

from pypdf import PdfReader, PdfWriter
from pypdf.generic import NameObject


SAFE_METADATA = {
    "/Title": "Provenance-Preserving Structure-Aware Chunking for Auditable Enterprise Retrieval-Augmented Generation",
    "/Subject": "Anonymous submission to ICAISG 2026",
    "/Keywords": "large language model safety, retrieval-augmented generation, explainable artificial intelligence, AI governance, data provenance, auditability, document chunking, trustworthy artificial intelligence",
    "/Author": "",
    "/Creator": "",
    "/Producer": "",
}


def main() -> None:
    parser = argparse.ArgumentParser(description="Remove identity-bearing PDF metadata while preserving pages.")
    parser.add_argument("pdf", type=Path)
    args = parser.parse_args()
    pdf = args.pdf.resolve()

    reader = PdfReader(pdf)
    writer = PdfWriter()
    writer.clone_document_from_reader(reader)
    writer.root_object.pop(NameObject("/Metadata"), None)
    writer.metadata = SAFE_METADATA

    fd, temporary_name = tempfile.mkstemp(prefix=f".{pdf.stem}-", suffix=".pdf", dir=pdf.parent)
    os.close(fd)
    temporary = Path(temporary_name)
    try:
        with temporary.open("wb") as stream:
            writer.write(stream)
        PdfReader(temporary)
        os.replace(temporary, pdf)
    finally:
        temporary.unlink(missing_ok=True)


if __name__ == "__main__":
    main()
