from __future__ import annotations

import argparse
import re
from pathlib import Path

from docx import Document
from docx.enum.table import WD_CELL_VERTICAL_ALIGNMENT, WD_TABLE_ALIGNMENT
from docx.enum.text import WD_ALIGN_PARAGRAPH, WD_LINE_SPACING, WD_TAB_ALIGNMENT
from docx.oxml import OxmlElement
from docx.oxml.ns import qn
from docx.shared import Cm, Inches, Pt, RGBColor

from lint_manuscript import BANNED_IDENTIFIERS


INLINE_PATTERN = re.compile(r"(\*\*.+?\*\*|`.+?`|\\\(.+?\\\)|(?<!\*)\*[^*]+?\*)")
TABLE_SEPARATOR = re.compile(r"^\|(?:\s*:?-+:?\s*\|)+$")
CJK_PATTERN = re.compile(r"[\u3400-\u9fff]")


def set_cell_shading(cell, fill: str) -> None:
    properties = cell._tc.get_or_add_tcPr()
    shading = properties.find(qn("w:shd"))
    if shading is None:
        shading = OxmlElement("w:shd")
        properties.append(shading)
    shading.set(qn("w:fill"), fill)


def remove_table_borders(table) -> None:
    properties = table._tbl.tblPr
    borders = properties.first_child_found_in("w:tblBorders")
    if borders is None:
        borders = OxmlElement("w:tblBorders")
        properties.append(borders)
    for name in ("top", "left", "bottom", "right", "insideH", "insideV"):
        edge = borders.find(qn(f"w:{name}"))
        if edge is None:
            edge = OxmlElement(f"w:{name}")
            borders.append(edge)
        edge.set(qn("w:val"), "nil")


def repeat_table_header(row) -> None:
    properties = row._tr.get_or_add_trPr()
    repeat = OxmlElement("w:tblHeader")
    repeat.set(qn("w:val"), "true")
    properties.append(repeat)


def set_table_width_percent(table, percent: int = 5000) -> None:
    properties = table._tbl.tblPr
    width = properties.first_child_found_in("w:tblW")
    if width is None:
        width = OxmlElement("w:tblW")
        properties.append(width)
    width.set(qn("w:w"), str(percent))
    width.set(qn("w:type"), "pct")


def parse_braced(text: str, start: int) -> tuple[str, int]:
    if start >= len(text) or text[start] != "{":
        raise ValueError("Expected a braced LaTeX group")
    depth = 0
    for index in range(start, len(text)):
        if text[index] == "{":
            depth += 1
        elif text[index] == "}":
            depth -= 1
            if depth == 0:
                return text[start + 1 : index], index + 1
    raise ValueError("Unclosed LaTeX group")


def replace_fractions(text: str) -> str:
    while "\\frac" in text:
        start = text.index("\\frac")
        numerator, after_numerator = parse_braced(text, start + len("\\frac"))
        denominator, after_denominator = parse_braced(text, after_numerator)
        replacement = f"({replace_fractions(numerator)})/({replace_fractions(denominator)})"
        text = text[:start] + replacement + text[after_denominator:]
    return text


def simplify_latex(value: str) -> tuple[str, str | None]:
    tag_match = re.search(r"\\tag\{(\d+)\}", value)
    tag = tag_match.group(1) if tag_match else None
    value = re.sub(r"\\tag\{\d+\}", "", value)
    value = replace_fractions(value)
    replacements = {
        "\\rho": "ρ",
        "\\theta": "θ",
        "\\sum": "∑",
        "\\max": "max",
        "\\min": "min",
        "\\cos": "cos",
        "\\ldots": "…",
        "\\Vert": "∥",
        "\\ne": "≠",
        "\\emptyset": "∅",
        "\\land": "∧",
        "\\lor": "∨",
        "\\in": "∈",
        "\\mathbb{1}": "𝟙",
        "\\;": " ",
        "\\,": " ",
        "\\left": "",
        "\\right": "",
    }
    for source, target in replacements.items():
        value = value.replace(source, target)
    value = re.sub(r"\\operatorname\{([^{}]+)\}", r"\1", value)
    value = re.sub(r"\\text\{([^{}]+)\}", r"\1", value)
    value = re.sub(r"\s+", " ", value.replace("\n", " ")).strip()
    value = value.replace(r"\{", "{").replace(r"\}", "}")
    value = value.replace("{", "(").replace("}", ")")
    return value, tag


def append_math(paragraph, formula: str, font_size: float = 10.5) -> None:
    """Add editable, renderer-stable linear mathematics.

    Word and LibreOffice interpret raw UnicodeMath stored in one math-text node
    differently, especially for long expressions. Normal text runs with true
    subscript formatting remain fully editable, render consistently, and keep
    equations from being flattened into images.
    """

    formula = formula.replace("-", "−")
    formula = re.sub(r"\s*([=+≠∧∨])\s*", r" \1 ", formula)
    formula = re.sub(r"\s+", " ", formula).strip()
    subscript = re.compile(r"_\(([^()]*)\)|_([A-Za-z0-9]+)")
    position = 0

    def add_run(value: str, is_subscript: bool = False) -> None:
        if not value:
            return
        run = paragraph.add_run(value)
        run.font.name = "Cambria Math"
        run.font.size = Pt(font_size - 1.5 if is_subscript else font_size)
        run.font.subscript = is_subscript

    for match in subscript.finditer(formula):
        add_run(formula[position : match.start()])
        add_run(match.group(1) or match.group(2), is_subscript=True)
        position = match.end()
    add_run(formula[position:])


def add_inline(paragraph, text: str, font_size: float | None = None) -> None:
    position = 0
    for match in INLINE_PATTERN.finditer(text):
        if match.start() > position:
            run = paragraph.add_run(text[position : match.start()])
            if font_size:
                run.font.size = Pt(font_size)
        token = match.group(0)
        if token.startswith("**"):
            run = paragraph.add_run(token[2:-2])
            run.bold = True
        elif token.startswith("`"):
            run = paragraph.add_run(token[1:-1])
            run.font.name = "Courier New"
            run.font.size = Pt((font_size or 10.5) - 0.5)
            run.font.color.rgb = RGBColor(60, 60, 60)
        elif token.startswith("\\("):
            formula, _ = simplify_latex(token[2:-2])
            append_math(paragraph, formula)
            position = match.end()
            continue
        else:
            run = paragraph.add_run(token[1:-1])
            run.italic = True
        if font_size:
            run.font.size = Pt(font_size)
        position = match.end()
    if position < len(text):
        run = paragraph.add_run(text[position:])
        if font_size:
            run.font.size = Pt(font_size)
    for run in paragraph.runs:
        if CJK_PATTERN.search(run.text):
            run.font.name = "Arial Unicode MS"
            fonts = run._element.get_or_add_rPr().get_or_add_rFonts()
            for family in ("ascii", "hAnsi", "eastAsia", "cs"):
                fonts.set(qn(f"w:{family}"), "Arial Unicode MS")


def add_equation(document: Document, latex: str) -> None:
    formula, number = simplify_latex(latex)
    table = document.add_table(rows=1, cols=2)
    table.alignment = WD_TABLE_ALIGNMENT.CENTER
    table.autofit = False
    set_table_width_percent(table)
    remove_table_borders(table)
    table.columns[0].width = Inches(5.8)
    table.columns[1].width = Inches(0.45)
    left = table.cell(0, 0).paragraphs[0]
    left.alignment = WD_ALIGN_PARAGRAPH.CENTER
    left.paragraph_format.space_after = Pt(2)
    append_math(left, formula, font_size=9.5)
    right = table.cell(0, 1).paragraphs[0]
    right.alignment = WD_ALIGN_PARAGRAPH.RIGHT
    right.paragraph_format.space_after = Pt(2)
    if number:
        right.add_run(f"({number})")


def add_markdown_table(document: Document, lines: list[str]) -> None:
    rows = []
    for line in lines:
        if TABLE_SEPARATOR.match(line.strip()):
            continue
        rows.append([cell.strip() for cell in line.strip().strip("|").split("|")])
    if not rows:
        return
    column_count = len(rows[0])
    if any(len(row) != column_count for row in rows):
        raise ValueError("Markdown table has inconsistent column counts")
    table = document.add_table(rows=len(rows), cols=column_count)
    table.style = "Table Grid"
    table.alignment = WD_TABLE_ALIGNMENT.CENTER
    table.autofit = True
    set_table_width_percent(table)
    for row_index, row in enumerate(rows):
        for column_index, value in enumerate(row):
            cell = table.cell(row_index, column_index)
            cell.vertical_alignment = WD_CELL_VERTICAL_ALIGNMENT.CENTER
            cell.text = ""
            paragraph = cell.paragraphs[0]
            paragraph.alignment = (
                WD_ALIGN_PARAGRAPH.LEFT if column_index == 0 else WD_ALIGN_PARAGRAPH.CENTER
            )
            paragraph.paragraph_format.space_after = Pt(0)
            add_inline(paragraph, value, font_size=7.5 if column_count >= 7 else 8.5)
            for run in paragraph.runs:
                if row_index == 0:
                    run.bold = True
                    run.font.color.rgb = RGBColor(255, 255, 255)
            if row_index == 0:
                set_cell_shading(cell, "1F4E79")
            elif row_index % 2 == 0:
                set_cell_shading(cell, "EAF2F8")
    repeat_table_header(table.rows[0])
    # Keep compact manuscript tables on one page when space permits. Word may
    # still split an exceptionally large table, but it will not strand a few
    # header/data rows at the bottom of the preceding page.
    for row in table.rows[:-1]:
        for cell in row.cells:
            for paragraph in cell.paragraphs:
                paragraph.paragraph_format.keep_with_next = True
    document.add_paragraph().paragraph_format.space_after = Pt(0)


def configure_document(document: Document) -> None:
    section = document.sections[0]
    section.page_width = Cm(21.0)
    section.page_height = Cm(29.7)
    section.top_margin = Cm(2.3)
    section.bottom_margin = Cm(2.3)
    section.left_margin = Cm(2.5)
    section.right_margin = Cm(2.5)

    styles = document.styles
    normal = styles["Normal"]
    normal.font.name = "Times New Roman"
    normal.font.size = Pt(10.5)
    normal._element.rPr.rFonts.set(qn("w:eastAsia"), "Arial Unicode MS")
    normal.paragraph_format.line_spacing_rule = WD_LINE_SPACING.SINGLE
    normal.paragraph_format.line_spacing = 1.08
    normal.paragraph_format.space_after = Pt(5)
    normal.paragraph_format.alignment = WD_ALIGN_PARAGRAPH.JUSTIFY

    title = styles["Title"]
    title.font.name = "Times New Roman"
    title.font.size = Pt(17)
    title.font.bold = True
    title.paragraph_format.alignment = WD_ALIGN_PARAGRAPH.CENTER
    title.paragraph_format.space_after = Pt(12)

    for name, size, before, after in (
        ("Heading 1", 14, 12, 5),
        ("Heading 2", 12, 10, 4),
        ("Heading 3", 11, 8, 3),
    ):
        style = styles[name]
        style.font.name = "Arial"
        style.font.size = Pt(size)
        style.font.bold = True
        style.font.color.rgb = RGBColor(31, 78, 121)
        style.paragraph_format.space_before = Pt(before)
        style.paragraph_format.space_after = Pt(after)
        style.paragraph_format.keep_with_next = True

    caption = styles["Caption"]
    caption.font.name = "Times New Roman"
    caption.font.size = Pt(9)
    caption.font.bold = True
    caption.font.italic = False
    caption.paragraph_format.alignment = WD_ALIGN_PARAGRAPH.CENTER
    caption.paragraph_format.space_before = Pt(6)
    caption.paragraph_format.space_after = Pt(4)
    caption.paragraph_format.keep_with_next = True

    document.core_properties.author = ""
    document.core_properties.last_modified_by = ""
    document.core_properties.title = "SEAC"
    document.core_properties.subject = "Software engineering document retrieval"
    document.core_properties.comments = ""

    footer = section.footer.paragraphs[0]
    footer.alignment = WD_ALIGN_PARAGRAPH.CENTER
    field = OxmlElement("w:fldSimple")
    field.set(qn("w:instr"), "PAGE")
    footer._p.append(field)


def add_paragraph(document: Document, text: str, style: str | None = None) -> None:
    if text.startswith("**Table ") and text.endswith("**"):
        paragraph = document.add_paragraph(style="Caption")
        add_inline(paragraph, text[2:-2])
        return
    paragraph = document.add_paragraph(style=style)
    add_inline(paragraph, text)
    if text.startswith("**Keywords:**"):
        paragraph.paragraph_format.alignment = WD_ALIGN_PARAGRAPH.LEFT
        paragraph.paragraph_format.space_after = Pt(10)
    if style in {"List Bullet", "List Number"}:
        paragraph.paragraph_format.alignment = WD_ALIGN_PARAGRAPH.LEFT
        paragraph.paragraph_format.space_after = Pt(2)


def add_numbered_paragraph(document: Document, text: str, number: int) -> None:
    """Render the source number explicitly so separate Markdown lists reset reliably."""

    paragraph = document.add_paragraph()
    paragraph.paragraph_format.alignment = WD_ALIGN_PARAGRAPH.LEFT
    paragraph.paragraph_format.left_indent = Cm(0.75)
    paragraph.paragraph_format.first_line_indent = Cm(-0.55)
    paragraph.paragraph_format.space_after = Pt(2)
    paragraph.add_run(f"{number}. ")
    add_inline(paragraph, text)


def markdown_to_docx(text: str) -> Document:
    document = Document()
    configure_document(document)
    lines = text.splitlines()
    index = 0
    paragraph_lines: list[str] = []

    def flush_paragraph() -> None:
        nonlocal paragraph_lines
        if paragraph_lines:
            add_paragraph(document, " ".join(line.strip() for line in paragraph_lines))
            paragraph_lines = []

    while index < len(lines):
        line = lines[index]
        stripped = line.strip()
        if not stripped:
            flush_paragraph()
            index += 1
            continue
        if stripped == "\\[":
            flush_paragraph()
            index += 1
            equation_lines = []
            while index < len(lines) and lines[index].strip() != "\\]":
                equation_lines.append(lines[index])
                index += 1
            if index >= len(lines):
                raise ValueError("Unclosed display equation")
            add_equation(document, "\n".join(equation_lines))
            index += 1
            continue
        if stripped.startswith("|"):
            flush_paragraph()
            table_lines = []
            while index < len(lines) and lines[index].strip().startswith("|"):
                table_lines.append(lines[index])
                index += 1
            add_markdown_table(document, table_lines)
            continue
        heading = re.match(r"^(#{1,4})\s+(.+)$", stripped)
        if heading:
            flush_paragraph()
            level = len(heading.group(1))
            title = heading.group(2)
            if level == 1:
                paragraph = document.add_paragraph(style="Title")
                add_inline(paragraph, title)
            else:
                paragraph = document.add_paragraph(style=f"Heading {min(level - 1, 3)}")
                add_inline(paragraph, title)
            index += 1
            continue
        bullet = re.match(r"^-\s+(.+)$", stripped)
        numbered = re.match(r"^(\d+)\.\s+(.+)$", stripped)
        if bullet or numbered:
            flush_paragraph()
            content = bullet.group(1) if bullet else numbered.group(2)
            index += 1
            continuations = []
            while index < len(lines) and lines[index].startswith("  ") and lines[index].strip():
                continuations.append(lines[index].strip())
                index += 1
            if continuations:
                content += " " + " ".join(continuations)
            if bullet:
                add_paragraph(document, content, "List Bullet")
            else:
                add_numbered_paragraph(document, content, int(numbered.group(1)))
            continue
        paragraph_lines.append(line)
        index += 1
    flush_paragraph()

    reference_heading_seen = False
    for paragraph in document.paragraphs:
        if paragraph.text == "References":
            reference_heading_seen = True
            continue
        if reference_heading_seen and paragraph.style.name == "Normal" and paragraph.text:
            paragraph.paragraph_format.left_indent = Cm(0.7)
            paragraph.paragraph_format.first_line_indent = Cm(-0.7)
            paragraph.paragraph_format.alignment = WD_ALIGN_PARAGRAPH.LEFT
            paragraph.paragraph_format.space_after = Pt(4)
    return document


def validate_submission_text(text: str, allow_layout_draft: bool) -> None:
    if "{{FORMAL_" in text:
        raise ValueError("Formal result placeholders remain")
    if "Author information withheld" in text:
        raise ValueError("Verified author information is missing")
    if "Anonymous layout draft" in text and not allow_layout_draft:
        raise ValueError("Anonymous layout draft cannot be emitted as a submission DOCX")
    for identifier in BANNED_IDENTIFIERS:
        if identifier in text:
            raise ValueError(f"Sensitive project identifier appears: {identifier}")
    bracket_placeholders = re.findall(r"\[(?:REPLACE|INSERT|ADD|CONFIRM)[^\]]*\]", text)
    if bracket_placeholders:
        raise ValueError(f"Human metadata placeholders remain: {bracket_placeholders[:3]}")
    if len(re.findall(r"\\tag\{\d+\}", text)) != 10:
        raise ValueError("Expected ten numbered editable equations")
    if len(re.findall(r"^\*\*Table \d+\.", text, flags=re.MULTILINE)) != 5:
        raise ValueError("Expected five editable numbered tables")


def main() -> None:
    parser = argparse.ArgumentParser(description="Build the single-column editable JSS submission DOCX.")
    parser.add_argument("--source", default="submission/JSS/seac_jss_manuscript.md")
    parser.add_argument("--output", default="submission/JSS/SEAC_JSS_submission.docx")
    parser.add_argument("--allow-layout-draft", action="store_true")
    parser.add_argument("--force", action="store_true")
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    source_path = Path(args.source) if Path(args.source).is_absolute() else root / args.source
    output_path = Path(args.output) if Path(args.output).is_absolute() else root / args.output
    if output_path.exists() and not args.force:
        raise FileExistsError(f"Output exists: {output_path}")
    text = source_path.read_text(encoding="utf-8")
    validate_submission_text(text, args.allow_layout_draft)
    document = markdown_to_docx(text)
    output_path.parent.mkdir(parents=True, exist_ok=True)
    document.save(output_path)
    print(output_path)


if __name__ == "__main__":
    main()
