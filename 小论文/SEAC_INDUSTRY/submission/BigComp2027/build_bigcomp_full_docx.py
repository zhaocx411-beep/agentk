#!/usr/bin/env python3
"""Build the anonymous 7--8 page BigComp full-paper draft."""

from __future__ import annotations

import json
import re
import shutil
import sys
import tempfile
import zipfile
from pathlib import Path

from docx import Document
from docx.enum.section import WD_SECTION_START
from docx.enum.table import WD_CELL_VERTICAL_ALIGNMENT
from docx.enum.text import WD_ALIGN_PARAGRAPH
from docx.oxml.ns import qn
from docx.shared import Inches, Pt
from PIL import Image, ImageDraw, ImageFont

import build_bigcomp_docx as base


SOURCE = base.ROOT / "submission/BigComp2027/paper_full_draft.md"
OUTPUT = base.ROOT / "submission/BigComp2027/seac_bigcomp2027_anonymous_full.docx"
ANALYSIS = (
    base.ROOT
    / "experiments/remote_formal_20260824/experiments/formal_analysis_locked/formal_analysis.json"
)
ARIAL = Path("/System/Library/Fonts/Supplemental/Arial.ttf")
ARIAL_BOLD = Path("/System/Library/Fonts/Supplemental/Arial Bold.ttf")


def load_font(size: int, bold: bool = False) -> ImageFont.FreeTypeFont:
    path = ARIAL_BOLD if bold else ARIAL
    if path.is_file():
        return ImageFont.truetype(str(path), size=size)
    return ImageFont.load_default()


def centered_text(
    draw: ImageDraw.ImageDraw,
    box: tuple[int, int, int, int],
    text: str,
    font: ImageFont.ImageFont,
    fill: str = "#111111",
) -> None:
    left, top, right, bottom = box
    lines = text.split("\n")
    bboxes = [draw.textbbox((0, 0), line, font=font) for line in lines]
    heights = [bbox[3] - bbox[1] for bbox in bboxes]
    total_height = sum(heights) + 7 * (len(lines) - 1)
    y = top + (bottom - top - total_height) / 2
    for line, bbox, height in zip(lines, bboxes, heights):
        width = bbox[2] - bbox[0]
        draw.text(((left + right - width) / 2, y), line, font=font, fill=fill)
        y += height + 7


def draw_arrow(
    draw: ImageDraw.ImageDraw,
    start: tuple[int, int],
    end: tuple[int, int],
    fill: str = "#333333",
    width: int = 5,
) -> None:
    draw.line((start, end), fill=fill, width=width)
    x, y = end
    draw.polygon([(x, y), (x - 13, y - 18), (x + 13, y - 18)], fill=fill)


def make_pipeline_figure(path: Path) -> None:
    image = Image.new("RGB", (1200, 1050), "white")
    draw = ImageDraw.Draw(image)
    title_font = load_font(32, bold=True)
    body_font = load_font(25)
    boxes = [
        ((115, 55, 1085, 185), "Enterprise artifacts", "DOCX/XLSX paragraphs, headings, and tables"),
        ((115, 245, 1085, 390), "Traceable units", "text + block ID + unit ID + heading path + protected group"),
        ((115, 450, 1085, 605), "Structure-preserving parents", "boundary score + adaptive threshold + protected assembly"),
        ((115, 665, 1085, 820), "Fine-grained retrieval children", "header-aware rows and contextual paragraph/sentence units"),
        ((115, 880, 1085, 1015), "Retrieved output", "coherent parent context + exact child/source provenance"),
    ]
    fills = ["#F4F4F4", "#E8EEF3", "#F4F4F4", "#E8EEF3", "#F4F4F4"]
    for index, ((left, top, right, bottom), title, subtitle) in enumerate(boxes):
        draw.rounded_rectangle(
            (left, top, right, bottom),
            radius=22,
            fill=fills[index],
            outline="#222222",
            width=4,
        )
        centered_text(draw, (left + 20, top + 10, right - 20, top + 68), title, title_font)
        centered_text(
            draw,
            (left + 25, top + 65, right - 25, bottom - 8),
            subtitle,
            body_font,
            fill="#303030",
        )
        if index < len(boxes) - 1:
            next_top = boxes[index + 1][0][1]
            draw_arrow(
                draw,
                ((left + right) // 2, bottom + 4),
                ((left + right) // 2, next_top - 8),
            )
    image.save(path, dpi=(300, 300), optimize=True)


def make_project_mrr_figure(path: Path) -> None:
    payload = json.loads(ANALYSIS.read_text(encoding="utf-8"))
    seac = payload["retrieval"]["proposed_v6"]["by_project"]
    llama = payload["retrieval"]["llamaindex_sentence"]["by_project"]
    projects = ["P04", "P05", "P06", "P07", "P08", "P09"]
    image = Image.new("RGB", (1200, 760), "white")
    draw = ImageDraw.Draw(image)
    label_font = load_font(25)
    bold_font = load_font(25, bold=True)
    small_font = load_font(21)
    left, right, top, bottom = 165, 1135, 85, 650
    x_min, x_max = 0.45, 1.0

    def x_coord(value: float) -> int:
        return int(left + (value - x_min) / (x_max - x_min) * (right - left))

    for tick in (0.5, 0.6, 0.7, 0.8, 0.9, 1.0):
        x = x_coord(tick)
        draw.line((x, top, x, bottom), fill="#D5D5D5", width=2)
        text = f"{tick:.1f}"
        bbox = draw.textbbox((0, 0), text, font=small_font)
        draw.text(
            (x - (bbox[2] - bbox[0]) / 2, bottom + 15),
            text,
            font=small_font,
            fill="#333333",
        )
    for index, project in enumerate(projects):
        y = top + 55 + index * 86
        draw.text((68, y - 14), project, font=bold_font, fill="#111111")
        x_seac = x_coord(seac[project]["mrr"])
        x_llama = x_coord(llama[project]["mrr"])
        draw.line((x_seac, y, x_llama, y), fill="#9A9A9A", width=5)
        draw.ellipse((x_seac - 11, y - 11, x_seac + 11, y + 11), fill="#111111")
        draw.rectangle(
            (x_llama - 10, y - 10, x_llama + 10, y + 10),
            fill="#AAAAAA",
            outline="#333333",
            width=2,
        )
    draw.line((left, bottom, right, bottom), fill="#222222", width=3)
    centered_text(draw, (left, 675, right, 744), "Unit MRR", bold_font)
    draw.ellipse((730, 24, 750, 44), fill="#111111")
    draw.text((762, 20), "Full SEAC", font=label_font, fill="#111111")
    draw.rectangle((940, 24, 960, 44), fill="#AAAAAA", outline="#333333", width=2)
    draw.text((972, 20), "LlamaIndex", font=label_font, fill="#111111")
    image.save(path, dpi=(300, 300), optimize=True)


def make_figure(key: str, path: Path) -> str:
    if key == "SEAC_PIPELINE":
        make_pipeline_figure(path)
        return "SEAC pipeline from enterprise artifacts to coherent parent context with exact provenance."
    if key == "PROJECT_MRR":
        make_project_mrr_figure(path)
        return "Per-project unit MRR comparison of full SEAC and LlamaIndex on P04 through P09."
    raise ValueError(f"unknown figure key: {key}")


def add_figure(document: Document, key: str, caption: str, directory: Path) -> None:
    image_path = directory / f"{key.lower()}.png"
    alt_text = make_figure(key, image_path)
    # Body Text uses a compressed line-height in the IEEE template, which clips
    # tall inline drawings.  A Normal paragraph lets Word derive its height from
    # the picture while preserving the template's one-column figure width.
    paragraph = document.add_paragraph(style="Normal")
    paragraph.alignment = WD_ALIGN_PARAGRAPH.CENTER
    paragraph.paragraph_format.space_before = Pt(3)
    paragraph.paragraph_format.space_after = Pt(2)
    paragraph.paragraph_format.keep_with_next = True
    shape = paragraph.add_run().add_picture(str(image_path), width=Inches(3.25))
    shape._inline.docPr.set("descr", alt_text)
    shape._inline.docPr.set("title", key.replace("_", " ").title())
    caption_paragraph = document.add_paragraph(base.clean_inline(caption), style="figure caption")
    caption_paragraph.alignment = WD_ALIGN_PARAGRAPH.CENTER
    caption_paragraph.paragraph_format.keep_together = True


def full_table_widths(caption: str, columns: int) -> list[int]:
    if "CORPUS" in caption:
        return [1050, 550, 750, 720, 790, 1180]
    if "PROJECT COVERAGE" in caption:
        return [650, 850, 750, 1570, 1220]
    if "BOUNDARY AND RETRIEVAL" in caption:
        return [1250, 800, 720, 650, 650, 970]
    if "QUESTION TYPE" in caption:
        return [1580, 430, 930, 850, 1250]
    if "ABLATIONS" in caption:
        return [1700, 800, 800, 650, 1090]
    if "RESOURCE COST" in caption:
        return [1260, 790, 850, 850, 1290]
    base_width, remainder = divmod(5040, columns)
    return [base_width + (1 if index < remainder else 0) for index in range(columns)]


def add_full_table(document: Document, rows: list[list[str]], caption: str) -> None:
    table = document.add_table(rows=len(rows), cols=len(rows[0]))
    base.set_table_geometry(table, full_table_widths(caption, len(rows[0])))
    base.set_repeat_header(table.rows[0])
    for row_index, values in enumerate(rows):
        for col_index, value in enumerate(values):
            cell = table.cell(row_index, col_index)
            cell.vertical_alignment = WD_CELL_VERTICAL_ALIGNMENT.CENTER
            paragraph = cell.paragraphs[0]
            paragraph.alignment = (
                WD_ALIGN_PARAGRAPH.LEFT if col_index == 0 else WD_ALIGN_PARAGRAPH.CENTER
            )
            paragraph.paragraph_format.space_before = Pt(0)
            paragraph.paragraph_format.space_after = Pt(0)
            paragraph.paragraph_format.line_spacing = 0.9
            base.prevent_midword_wrap(paragraph)
            run = paragraph.add_run(base.clean_inline(value))
            run.font.name = "Times New Roman"
            run._element.get_or_add_rPr().rFonts.set(qn("w:ascii"), "Times New Roman")
            run._element.get_or_add_rPr().rFonts.set(qn("w:hAnsi"), "Times New Roman")
            run.font.size = Pt(7.2)
            run.bold = row_index == 0 or "**" in value
    after = document.add_paragraph(style="Body Text")
    after.paragraph_format.space_after = Pt(0)
    after.paragraph_format.line_spacing = 0.5


def add_full_equation(document: Document, block: str) -> None:
    if "S(p,q)" not in block:
        base.add_equation(document, block)
        return
    paragraph = document.add_paragraph(style="equation")
    paragraph.alignment = WD_ALIGN_PARAGRAPH.CENTER
    paragraph.paragraph_format.space_before = Pt(2)
    paragraph.paragraph_format.space_after = Pt(3)
    run = paragraph.add_run("S(p,q) = 0.35Sₚ(q) + 0.65 max S꜀(q)")
    run.font.name = "Cambria Math"
    run._element.get_or_add_rPr().rFonts.set(qn("w:ascii"), "Cambria Math")
    run._element.get_or_add_rPr().rFonts.set(qn("w:hAnsi"), "Cambria Math")
    run.font.size = Pt(10)


def scrub_template_footer_placeholders(path: Path) -> None:
    """Blank orphaned sample footer text without rebuilding OOXML relationships."""
    with zipfile.ZipFile(path, "r") as source_zip:
        parts = {name: source_zip.read(name) for name in source_zip.namelist()}
    placeholders = (
        b"XXX-X-XXXX-XXXX-X/XX/$XX.00 \xc2\xa920XX IEEE",
        b"XXX-X-XXXX-XXXX-X/XX/$XX.00 &#169;20XX IEEE",
    )
    for name in list(parts):
        if name.startswith("word/footer") and name.endswith(".xml"):
            for placeholder in placeholders:
                parts[name] = parts[name].replace(placeholder, b"")
    temporary = path.with_suffix(".tmp.docx")
    with zipfile.ZipFile(temporary, "w", compression=zipfile.ZIP_DEFLATED) as output_zip:
        for name, data in parts.items():
            output_zip.writestr(name, data)
    temporary.replace(path)


def build() -> Path:
    if not base.TEMPLATE.is_file():
        raise FileNotFoundError(base.TEMPLATE)
    if not SOURCE.is_file():
        raise FileNotFoundError(SOURCE)
    if not ANALYSIS.is_file():
        raise FileNotFoundError(ANALYSIS)
    shutil.copyfile(base.TEMPLATE, OUTPUT)
    document = Document(OUTPUT)
    base.clear_body(document)

    blocks = [
        block.strip()
        for block in SOURCE.read_text(encoding="utf-8").split("\n\n")
        if block.strip()
    ]
    title = base.clean_inline(blocks[0].removeprefix("# "))
    author = base.clean_inline(blocks[1])

    first = document.sections[0]
    base.configure_section(first, 1)
    title_paragraph = document.add_paragraph(title, style="paper title")
    title_paragraph.paragraph_format.keep_with_next = True
    author_paragraph = document.add_paragraph(author, style="Author")
    author_paragraph.paragraph_format.space_before = Pt(0)
    author_paragraph.paragraph_format.space_after = Pt(6)

    body_section = document.add_section(WD_SECTION_START.CONTINUOUS)
    base.configure_section(body_section, 2)

    index = 2
    current_caption = ""
    with tempfile.TemporaryDirectory(prefix="bigcomp_figures_", dir="/private/tmp") as directory:
        figure_directory = Path(directory)
        while index < len(blocks):
            block = blocks[index]
            if block == "## Abstract":
                index += 1
                paragraph = document.add_paragraph(style="Abstract")
                label = paragraph.add_run("Abstract—")
                label.bold = True
                label.italic = True
                body = paragraph.add_run(base.clean_inline(blocks[index]))
                body.bold = False
                body.italic = False
                index += 1
                continue
            if block.startswith("**Keywords—**"):
                paragraph = document.add_paragraph(style="Keywords")
                label = paragraph.add_run("Keywords—")
                label.bold = True
                label.italic = True
                terms = base.clean_inline(block.replace("**Keywords—**", "", 1))
                body = paragraph.add_run(terms)
                body.bold = False
                body.italic = False
                index += 1
                continue
            if block.startswith("## "):
                heading = re.sub(r"^##\s+(?:[IVX]+\.\s+)?", "", block)
                style = "Heading 5" if heading in {"Acknowledgment", "References"} else "Heading 1"
                document.add_paragraph(heading, style=style)
                index += 1
                continue
            if block.startswith("### "):
                heading = re.sub(r"^###\s+(?:[A-Z]\.\s+)?", "", block)
                document.add_paragraph(heading, style="Heading 2")
                index += 1
                continue
            if block.startswith("**TABLE"):
                current_caption = base.clean_inline(
                    re.sub(r"^\*\*TABLE\s+[IVX]+\.\s*", "", block)
                )
                document.add_paragraph(current_caption, style="table head")
                index += 1
                continue
            if block.startswith("|"):
                add_full_table(document, base.parse_table(block), current_caption)
                current_caption = ""
                index += 1
                continue
            if block.startswith("[[FIGURE:"):
                match = re.fullmatch(
                    r"\[\[FIGURE:([A-Z0-9_]+)\|(.+)\]\]", block, re.DOTALL
                )
                if not match:
                    raise ValueError(f"invalid figure marker: {block}")
                add_figure(document, match.group(1), match.group(2), figure_directory)
                index += 1
                continue
            if block.startswith("\\["):
                add_full_equation(document, block)
                index += 1
                continue
            if re.match(r"^1\.\s", block):
                base.add_numbered_contributions(document, block)
                index += 1
                continue
            if re.match(r"^\[\d+\]\s", block):
                reference = re.sub(r"^\[\d+\]\s*", "", base.clean_inline(block))
                paragraph = document.add_paragraph(reference, style="references")
                paragraph.paragraph_format.keep_together = True
                index += 1
                continue
            if block == "The contributions are:":
                index += 1
                continue
            document.add_paragraph(base.clean_inline(block), style="Body Text")
            index += 1

    for paragraph in document.paragraphs:
        base.prevent_midword_wrap(paragraph)
        for run in paragraph.runs:
            base.set_run_language(run)
    for table in document.tables:
        for row in table.rows:
            for cell in row.cells:
                for paragraph in cell.paragraphs:
                    base.prevent_midword_wrap(paragraph)
                    for run in paragraph.runs:
                        base.set_run_language(run)

    document.core_properties.author = ""
    document.core_properties.last_modified_by = ""
    document.core_properties.company = ""
    document.core_properties.title = "Anonymous BigComp full-paper submission"
    document.core_properties.subject = "Anonymous conference manuscript"
    document.core_properties.keywords = ""
    document.core_properties.comments = ""
    document.save(OUTPUT)
    scrub_template_footer_placeholders(OUTPUT)
    return OUTPUT


if __name__ == "__main__":
    try:
        print(build())
    except Exception as exc:
        print(f"build failed: {exc}", file=sys.stderr)
        raise
