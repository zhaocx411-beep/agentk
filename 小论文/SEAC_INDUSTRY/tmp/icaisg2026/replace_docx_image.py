from __future__ import annotations

import argparse
import os
import shutil
import tempfile
import zipfile
from pathlib import Path


def main() -> None:
    parser = argparse.ArgumentParser(description="Replace one embedded DOCX image without changing its drawing geometry.")
    parser.add_argument("docx", type=Path)
    parser.add_argument("image", type=Path)
    parser.add_argument("--member", default="word/media/image1.png")
    parser.add_argument("--backup", type=Path, required=True)
    parser.add_argument("--figure-copy", type=Path, required=True)
    args = parser.parse_args()

    docx = args.docx.resolve()
    image = args.image.resolve()
    backup = args.backup.resolve()
    figure_copy = args.figure_copy.resolve()

    if not docx.is_file():
        raise FileNotFoundError(docx)
    if not image.is_file():
        raise FileNotFoundError(image)
    if image.read_bytes()[:8] != b"\x89PNG\r\n\x1a\n":
        raise ValueError(f"Not a PNG file: {image}")

    backup.parent.mkdir(parents=True, exist_ok=True)
    figure_copy.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(docx, backup)
    shutil.copy2(image, figure_copy)

    with zipfile.ZipFile(docx, "r") as source_zip:
        names = set(source_zip.namelist())
        if args.member not in names:
            raise KeyError(f"Embedded image not found: {args.member}")
        fd, temporary_name = tempfile.mkstemp(prefix=f".{docx.stem}-", suffix=".docx", dir=docx.parent)
        os.close(fd)
        temporary = Path(temporary_name)
        try:
            with zipfile.ZipFile(temporary, "w") as target_zip:
                for info in source_zip.infolist():
                    payload = image.read_bytes() if info.filename == args.member else source_zip.read(info.filename)
                    target_zip.writestr(info, payload)
            os.replace(temporary, docx)
        finally:
            temporary.unlink(missing_ok=True)


if __name__ == "__main__":
    main()
