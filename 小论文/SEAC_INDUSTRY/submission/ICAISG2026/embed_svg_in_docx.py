from __future__ import annotations

import argparse
import os
import re
import tempfile
import zipfile
from pathlib import Path


SVG_BLIP_URI = "{96DAC541-7B7A-43D3-8B79-37D633B846F1}"
SVG_NAMESPACE = "http://schemas.microsoft.com/office/drawing/2016/SVG/main"
IMAGE_RELATIONSHIP = (
    "http://schemas.openxmlformats.org/officeDocument/2006/relationships/image"
)


def _next_relationship_id(rels_xml: str) -> str:
    values = [int(value) for value in re.findall(r'Id="rId(\d+)"', rels_xml)]
    return f"rId{max(values, default=0) + 1}"


def embed_svg_with_png_fallback(
    docx_path: Path,
    svg_path: Path,
    *,
    fallback_target: str = "media/image1.png",
    svg_target: str = "media/image2.svg",
) -> None:
    """Make an existing PNG drawing use SVG in Office 2016+.

    The original PNG relationship and drawing geometry remain unchanged, so older
    Word versions retain a compatible fallback while modern Word and PDF export
    use the vector artwork.
    """
    docx_path = Path(docx_path)
    svg_path = Path(svg_path)
    if not docx_path.exists():
        raise FileNotFoundError(docx_path)
    if not svg_path.exists():
        raise FileNotFoundError(svg_path)

    with zipfile.ZipFile(docx_path, "r") as source:
        entries = {info.filename: (info, source.read(info.filename)) for info in source.infolist()}
        archive_comment = source.comment

    rels_name = "word/_rels/document.xml.rels"
    document_name = "word/document.xml"
    content_types_name = "[Content_Types].xml"
    for required in (rels_name, document_name, content_types_name):
        if required not in entries:
            raise ValueError(f"DOCX is missing {required}")

    rels_xml = entries[rels_name][1].decode("utf-8")
    document_xml = entries[document_name][1].decode("utf-8")
    content_types_xml = entries[content_types_name][1].decode("utf-8")

    image_relationships = re.findall(r"<Relationship\b[^>]*/>", rels_xml)
    fallback_rel = next(
        (
            relationship
            for relationship in image_relationships
            if f'Type="{IMAGE_RELATIONSHIP}"' in relationship
            and f'Target="{fallback_target}"' in relationship
        ),
        None,
    )
    if fallback_rel is None:
        raise ValueError(f"No PNG fallback relationship targets {fallback_target}")
    fallback_id_match = re.search(r'Id="([^"]+)"', fallback_rel)
    if fallback_id_match is None:
        raise ValueError("PNG fallback relationship has no Id")
    fallback_id = fallback_id_match.group(1)

    svg_rel = next(
        (
            relationship
            for relationship in image_relationships
            if f'Type="{IMAGE_RELATIONSHIP}"' in relationship
            and f'Target="{svg_target}"' in relationship
        ),
        None,
    )
    if svg_rel:
        svg_id_match = re.search(r'Id="([^"]+)"', svg_rel)
        if svg_id_match is None:
            raise ValueError("SVG relationship has no Id")
        svg_id = svg_id_match.group(1)
    else:
        svg_id = _next_relationship_id(rels_xml)
        relationship = (
            f'<Relationship Id="{svg_id}" Type="{IMAGE_RELATIONSHIP}" '
            f'Target="{svg_target}"/>'
        )
        rels_xml = rels_xml.replace("</Relationships>", f"{relationship}</Relationships>")

    if 'Extension="svg"' not in content_types_xml:
        svg_content_type = '<Default Extension="svg" ContentType="image/svg+xml"/>'
        content_types_xml = content_types_xml.replace("</Types>", f"{svg_content_type}</Types>")

    if "<asvg:svgBlip" in document_xml:
        document_xml, substitutions = re.subn(
            r'(<asvg:svgBlip\b[^>]*\br:embed=")[^"]+("[^>]*/>)',
            rf"\g<1>{svg_id}\g<2>",
            document_xml,
            count=1,
        )
    else:
        svg_extension = (
            f'<a:extLst><a:ext uri="{SVG_BLIP_URI}">'
            f'<asvg:svgBlip xmlns:asvg="{SVG_NAMESPACE}" r:embed="{svg_id}"/>'
            "</a:ext></a:extLst>"
        )
        pattern = rf'(<a:blip\b[^>]*\br:embed="{re.escape(fallback_id)}"[^>]*)/>'
        document_xml, substitutions = re.subn(
            pattern,
            rf"\g<1>>{svg_extension}</a:blip>",
            document_xml,
            count=1,
        )
    if substitutions != 1:
        raise ValueError("Could not patch exactly one drawing with the SVG extension")

    replacements = {
        rels_name: rels_xml.encode("utf-8"),
        document_name: document_xml.encode("utf-8"),
        content_types_name: content_types_xml.encode("utf-8"),
        f"word/{svg_target}": svg_path.read_bytes(),
    }

    fd, temporary_name = tempfile.mkstemp(suffix=".docx", dir=docx_path.parent)
    os.close(fd)
    temporary_path = Path(temporary_name)
    try:
        with zipfile.ZipFile(temporary_path, "w") as target:
            target.comment = archive_comment
            for filename, (info, data) in entries.items():
                target.writestr(info, replacements.pop(filename, data))
            for filename, data in replacements.items():
                info = zipfile.ZipInfo(filename, date_time=(2012, 7, 2, 9, 52, 0))
                info.compress_type = zipfile.ZIP_DEFLATED
                info.external_attr = 0o600 << 16
                target.writestr(info, data)
        os.replace(temporary_path, docx_path)
    finally:
        if temporary_path.exists():
            temporary_path.unlink()


def main() -> None:
    parser = argparse.ArgumentParser(description="Embed SVG into a DOCX with PNG fallback")
    parser.add_argument("docx", type=Path)
    parser.add_argument("svg", type=Path)
    args = parser.parse_args()
    embed_svg_with_png_fallback(args.docx, args.svg)


if __name__ == "__main__":
    main()
