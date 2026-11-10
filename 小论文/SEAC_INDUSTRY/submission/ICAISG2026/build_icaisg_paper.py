from __future__ import annotations

import re
from pathlib import Path

from docx import Document
from docx.enum.section import WD_SECTION
from docx.enum.table import WD_CELL_VERTICAL_ALIGNMENT, WD_TABLE_ALIGNMENT
from docx.enum.text import WD_ALIGN_PARAGRAPH, WD_BREAK
from docx.oxml import OxmlElement
from docx.oxml.ns import qn
from docx.shared import Inches, Pt, RGBColor

from embed_svg_in_docx import embed_svg_with_png_fallback


ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / "submission/ICAISG2026/ICAISG2026_SEAC_blind_source.md"
TEMPLATE = ROOT / "tmp/icaisg2026/transitional/ICAISG2026_official_template.docx"
WORKING = ROOT / "tmp/icaisg2026/ICAISG2026_SEAC_blind_working.docx"
FIGURE = ROOT / "submission/ICAISG2026/package/seac_pipeline.png"
SVG_FIGURE = ROOT / "submission/ICAISG2026/package/seac_pipeline.svg"


def set_font(run, size: float, *, bold=None, italic=None, small_caps=None) -> None:
    run.font.name = "Times New Roman"
    run._element.get_or_add_rPr().get_or_add_rFonts().set(qn("w:ascii"), "Times New Roman")
    run._element.get_or_add_rPr().get_or_add_rFonts().set(qn("w:hAnsi"), "Times New Roman")
    run.font.size = Pt(size)
    if bold is not None:
        run.bold = bold
    if italic is not None:
        run.italic = italic
    if small_caps is not None:
        run.font.small_caps = small_caps


def configure_styles(doc: Document) -> None:
    for name in ("Normal", "Body Text"):
        style = doc.styles[name]
        style.font.name = "Times New Roman"
        style.font.size = Pt(10)
        style.paragraph_format.alignment = WD_ALIGN_PARAGRAPH.JUSTIFY
        style.paragraph_format.first_line_indent = Inches(0.20)
        style.paragraph_format.space_before = Pt(0)
        style.paragraph_format.space_after = Pt(0)
        style.paragraph_format.line_spacing = 1.0

    for name in ("Heading 1", "Heading 2"):
        style = doc.styles[name]
        num_pr = style.element.pPr.find(qn("w:numPr"))
        if num_pr is not None:
            style.element.pPr.remove(num_pr)

    heading1 = doc.styles["Heading 1"]
    heading1.font.name = "Times New Roman"
    heading1.font.size = Pt(10)
    heading1.font.bold = False
    heading1.font.small_caps = True
    heading1.paragraph_format.alignment = WD_ALIGN_PARAGRAPH.CENTER
    heading1.paragraph_format.space_before = Pt(8)
    heading1.paragraph_format.space_after = Pt(4)
    heading1.paragraph_format.keep_with_next = True

    heading2 = doc.styles["Heading 2"]
    heading2.font.name = "Times New Roman"
    heading2.font.size = Pt(10)
    heading2.font.bold = False
    heading2.font.italic = True
    heading2.paragraph_format.alignment = WD_ALIGN_PARAGRAPH.LEFT
    heading2.paragraph_format.space_before = Pt(6)
    heading2.paragraph_format.space_after = Pt(3)
    heading2.paragraph_format.keep_with_next = True

    heading5 = doc.styles["Heading 5"]
    heading5.font.name = "Times New Roman"
    heading5.font.size = Pt(10)
    heading5.font.bold = False
    heading5.font.small_caps = True
    heading5.paragraph_format.alignment = WD_ALIGN_PARAGRAPH.CENTER
    heading5.paragraph_format.space_before = Pt(8)
    heading5.paragraph_format.space_after = Pt(4)
    heading5.paragraph_format.keep_with_next = True

    title = doc.styles["paper title"]
    title.font.name = "Times New Roman"
    title.font.size = Pt(24)
    title.font.bold = False
    title.paragraph_format.alignment = WD_ALIGN_PARAGRAPH.CENTER
    title.paragraph_format.space_before = Pt(0)
    title.paragraph_format.space_after = Pt(12)
    title.paragraph_format.keep_with_next = True

    for name in ("Abstract", "Keywords"):
        style = doc.styles[name]
        style.font.name = "Times New Roman"
        style.font.size = Pt(9)
        style.paragraph_format.alignment = WD_ALIGN_PARAGRAPH.JUSTIFY
        style.paragraph_format.first_line_indent = Inches(0.19)
        style.paragraph_format.space_before = Pt(0)
        style.paragraph_format.space_after = Pt(5 if name == "Abstract" else 3)
        style.paragraph_format.line_spacing = 1.0

    ref_style = doc.styles["references"]
    ref_style.font.name = "Times New Roman"
    ref_style.font.size = Pt(8)
    ref_style.paragraph_format.alignment = WD_ALIGN_PARAGRAPH.JUSTIFY
    ref_style.paragraph_format.left_indent = Inches(0.22)
    ref_style.paragraph_format.first_line_indent = Inches(-0.22)
    ref_style.paragraph_format.space_before = Pt(0)
    ref_style.paragraph_format.space_after = Pt(1)
    ref_style.paragraph_format.line_spacing = 1.0
    ref_num_pr = ref_style.element.pPr.find(qn("w:numPr"))
    if ref_num_pr is not None:
        ref_style.element.pPr.remove(ref_num_pr)


def clear_body_keep_last_section(doc: Document) -> None:
    body = doc._body._element
    sect_pr = body.sectPr
    for child in list(body):
        if child is not sect_pr:
            body.remove(child)


def set_columns(section, number: int, space_twips: int = 360) -> None:
    sect_pr = section._sectPr
    cols = sect_pr.find(qn("w:cols"))
    if cols is None:
        cols = OxmlElement("w:cols")
        sect_pr.append(cols)
    cols.set(qn("w:num"), str(number))
    cols.set(qn("w:space"), str(space_twips))
    cols.set(qn("w:equalWidth"), "1")


def configure_section(section, columns: int) -> None:
    section.page_width = Inches(8.5)
    section.page_height = Inches(11)
    section.top_margin = Inches(0.75)
    section.bottom_margin = Inches(1.0)
    section.left_margin = Inches(0.625)
    section.right_margin = Inches(0.625)
    section.header_distance = Inches(0.5)
    section.footer_distance = Inches(0.5)
    section.different_first_page_header_footer = False
    set_columns(section, columns)
    for story in (section.header, section.footer):
        for paragraph in story.paragraphs:
            paragraph.clear()


def normalize_inline(text: str) -> str:
    text = text.strip()
    text = re.sub(r"\*\*(.*?)\*\*", r"\1", text)
    text = re.sub(r"\*(.*?)\*", r"\1", text)
    text = text.replace("\\(", "").replace("\\)", "")
    replacements = {
        "\\rho": "rho",
        "\\theta": "theta",
        "\\cos": "cos",
        "\\Vert": "||",
        "\\in": "in",
        "\\ldots": "...",
        "\\text": "",
    }
    for old, new in replacements.items():
        text = text.replace(old, new)
    return text.replace("{", "").replace("}", "")


def add_body(doc: Document, text: str, *, size: float = 10) -> None:
    p = doc.add_paragraph(style="Body Text")
    p.paragraph_format.widow_control = True
    run = p.add_run(normalize_inline(text))
    set_font(run, size)


def add_heading1(doc: Document, text: str) -> None:
    p = doc.add_paragraph(style="Heading 1")
    p.paragraph_format.keep_with_next = True
    run = p.add_run(text.upper())
    set_font(run, 10, bold=False, small_caps=True)


def add_heading2(doc: Document, text: str) -> None:
    p = doc.add_paragraph(style="Heading 2")
    p.paragraph_format.keep_with_next = True
    run = p.add_run(text)
    set_font(run, 10, italic=True)


def add_component_heading(doc: Document, text: str) -> None:
    p = doc.add_paragraph(style="Heading 1")
    p.paragraph_format.keep_with_next = True
    run = p.add_run(text.upper())
    set_font(run, 10, small_caps=True)


def add_abstract(doc: Document, text: str) -> None:
    p = doc.add_paragraph(style="Abstract")
    label = p.add_run("Abstract-")
    set_font(label, 9, bold=True, italic=True)
    run = p.add_run(normalize_inline(text))
    set_font(run, 9, bold=True)


def add_keywords(doc: Document, text: str) -> None:
    p = doc.add_paragraph(style="Keywords")
    label = p.add_run("Keywords-")
    set_font(label, 9, bold=True, italic=True)
    run = p.add_run(normalize_inline(text))
    set_font(run, 9, italic=True)


def add_equation(doc: Document, lines: list[str]) -> None:
    latex = " ".join(line.strip() for line in lines)
    if "t(r_j)" in latex:
        text = "t(r_j) = header(h) || row(r_j)"
    elif "rho(u)" in latex:
        text = "rho(u) = sum(v in V) count(v,u) / max(1, tokens(u))"
    elif "b_i" in latex:
        text = "b_i = 0.45 d_i + 0.30 s_i + 0.25 k_i - p_i"
    elif "S(p,q)" in latex:
        text = "S(p,q) = 0.35 S_p(q) + 0.65 max S_c(q)"
    else:
        text = normalize_inline(latex)
    p = doc.add_paragraph()
    p.alignment = WD_ALIGN_PARAGRAPH.CENTER
    p.paragraph_format.space_before = Pt(2)
    p.paragraph_format.space_after = Pt(2)
    run = p.add_run(text)
    set_font(run, 10, italic=True)


def set_repeat_table_header(row) -> None:
    tr_pr = row._tr.get_or_add_trPr()
    tbl_header = OxmlElement("w:tblHeader")
    tbl_header.set(qn("w:val"), "true")
    tr_pr.append(tbl_header)


def prevent_row_split(row) -> None:
    tr_pr = row._tr.get_or_add_trPr()
    cant_split = OxmlElement("w:cantSplit")
    cant_split.set(qn("w:val"), "true")
    tr_pr.append(cant_split)


def set_cell_margins(cell, top=35, start=45, bottom=35, end=45) -> None:
    tc_pr = cell._tc.get_or_add_tcPr()
    tc_mar = tc_pr.first_child_found_in("w:tcMar")
    if tc_mar is None:
        tc_mar = OxmlElement("w:tcMar")
        tc_pr.append(tc_mar)
    for name, value in (("top", top), ("start", start), ("bottom", bottom), ("end", end)):
        node = tc_mar.find(qn(f"w:{name}"))
        if node is None:
            node = OxmlElement(f"w:{name}")
            tc_mar.append(node)
        node.set(qn("w:w"), str(value))
        node.set(qn("w:type"), "dxa")


def set_table_borders(table) -> None:
    tbl_pr = table._tbl.tblPr
    borders = tbl_pr.find(qn("w:tblBorders"))
    if borders is None:
        borders = OxmlElement("w:tblBorders")
        tbl_pr.append(borders)
    for edge in ("top", "left", "bottom", "right", "insideH", "insideV"):
        node = borders.find(qn(f"w:{edge}"))
        if node is None:
            node = OxmlElement(f"w:{edge}")
            borders.append(node)
        node.set(qn("w:val"), "single")
        node.set(qn("w:sz"), "4")
        node.set(qn("w:color"), "000000")


def set_table_geometry(table, widths: list[float]) -> None:
    """Pin table and cell widths so LibreOffice cannot clip edge columns."""
    twips = [round(width * 1440) for width in widths]
    total = sum(twips)
    tbl_pr = table._tbl.tblPr

    tbl_w = tbl_pr.find(qn("w:tblW"))
    if tbl_w is None:
        tbl_w = OxmlElement("w:tblW")
        tbl_pr.append(tbl_w)
    tbl_w.set(qn("w:w"), str(total))
    tbl_w.set(qn("w:type"), "dxa")

    layout = tbl_pr.find(qn("w:tblLayout"))
    if layout is None:
        layout = OxmlElement("w:tblLayout")
        tbl_pr.append(layout)
    layout.set(qn("w:type"), "fixed")

    indent = tbl_pr.find(qn("w:tblInd"))
    if indent is None:
        indent = OxmlElement("w:tblInd")
        tbl_pr.append(indent)
    indent.set(qn("w:w"), "0")
    indent.set(qn("w:type"), "dxa")

    grid = table._tbl.tblGrid
    for child in list(grid):
        grid.remove(child)
    for width in twips:
        col = OxmlElement("w:gridCol")
        col.set(qn("w:w"), str(width))
        grid.append(col)

    for row in table.rows:
        for cell, width in zip(row.cells, twips):
            tc_pr = cell._tc.get_or_add_tcPr()
            tc_w = tc_pr.find(qn("w:tcW"))
            if tc_w is None:
                tc_w = OxmlElement("w:tcW")
                tc_pr.append(tc_w)
            tc_w.set(qn("w:w"), str(width))
            tc_w.set(qn("w:type"), "dxa")


def width_plan(headers: list[str]) -> list[float]:
    key = tuple(headers)
    plans = {
        ("Split", "Projects", "Documents", "Units", "Regex tokens", "Table rows"): [0.78, 0.45, 0.50, 0.54, 0.64, 0.59],
        ("Method", "Unit MRR", "Hit@5", "Span MRR", "Boundary F1"): [1.10, 0.56, 0.56, 0.60, 0.68],
        ("Variant", "Unit MRR", "Hit@5", "Change from full"): [1.38, 0.62, 0.55, 0.95],
    }
    return plans.get(key, [3.5 / len(headers)] * len(headers))


def add_table(doc: Document, caption: str, rows: list[list[str]]) -> None:
    cap = doc.add_paragraph()
    cap.alignment = WD_ALIGN_PARAGRAPH.CENTER
    cap.paragraph_format.space_before = Pt(5)
    cap.paragraph_format.space_after = Pt(2)
    cap.paragraph_format.keep_with_next = True
    run = cap.add_run(normalize_inline(caption))
    set_font(run, 8, small_caps=True)

    headers, data = rows[0], rows[1:]
    table = doc.add_table(rows=1, cols=len(headers))
    table.alignment = WD_TABLE_ALIGNMENT.CENTER
    table.autofit = False
    widths = width_plan(headers)
    for i, text in enumerate(headers):
        cell = table.rows[0].cells[i]
        cell.width = Inches(widths[i])
        cell.vertical_alignment = WD_CELL_VERTICAL_ALIGNMENT.CENTER
        set_cell_margins(cell)
        p = cell.paragraphs[0]
        p.alignment = WD_ALIGN_PARAGRAPH.CENTER
        p.paragraph_format.space_before = Pt(0)
        p.paragraph_format.space_after = Pt(0)
        run = p.add_run(text)
        set_font(run, 7.0, bold=True)
    set_repeat_table_header(table.rows[0])
    prevent_row_split(table.rows[0])

    for values in data:
        row = table.add_row()
        prevent_row_split(row)
        for i, text in enumerate(values):
            cell = row.cells[i]
            cell.width = Inches(widths[i])
            cell.vertical_alignment = WD_CELL_VERTICAL_ALIGNMENT.CENTER
            set_cell_margins(cell)
            p = cell.paragraphs[0]
            p.alignment = WD_ALIGN_PARAGRAPH.LEFT if i == 0 else WD_ALIGN_PARAGRAPH.CENTER
            p.paragraph_format.space_before = Pt(0)
            p.paragraph_format.space_after = Pt(0)
            run = p.add_run(text)
            set_font(run, 6.6)
    set_table_geometry(table, widths)
    set_table_borders(table)
    spacer = doc.add_paragraph()
    spacer.paragraph_format.space_after = Pt(1)


def create_pipeline_figure() -> None:
    if not FIGURE.exists():
        raise FileNotFoundError(f"Missing publication figure: {FIGURE}")
    if not SVG_FIGURE.exists():
        raise FileNotFoundError(f"Missing vector publication figure: {SVG_FIGURE}")


def add_figure(doc: Document) -> None:
    create_pipeline_figure()
    p = doc.add_paragraph()
    p.alignment = WD_ALIGN_PARAGRAPH.CENTER
    p.paragraph_format.space_before = Pt(3)
    p.paragraph_format.space_after = Pt(1)
    run = p.add_run()
    run.add_picture(str(FIGURE), width=Inches(3.45))
    doc_pr = run._element.xpath(".//wp:docPr")
    if doc_pr:
        doc_pr[0].set("descr", "SEAC pipeline from parsed provenance units to auditable parent retrieval")
    cap = doc.add_paragraph()
    cap.alignment = WD_ALIGN_PARAGRAPH.JUSTIFY
    cap.paragraph_format.first_line_indent = Inches(0)
    cap.paragraph_format.space_before = Pt(0)
    cap.paragraph_format.space_after = Pt(3)
    caption = cap.add_run("Fig. 1. SEAC preserves stable source identifiers through boundary scoring, parent construction, and child-triggered retrieval.")
    set_font(caption, 8)


def parse_table(lines: list[str], index: int) -> tuple[list[list[str]], int]:
    rows: list[list[str]] = []
    while index < len(lines) and lines[index].strip().startswith("|"):
        values = [item.strip() for item in lines[index].strip().strip("|").split("|")]
        if not all(re.fullmatch(r":?-+:?", value) for value in values):
            rows.append(values)
        index += 1
    return rows, index


def build() -> None:
    doc = Document(str(TEMPLATE))
    clear_body_keep_last_section(doc)
    configure_styles(doc)
    configure_section(doc.sections[0], 1)

    lines = SOURCE.read_text(encoding="utf-8").splitlines()
    title_text = lines[0].removeprefix("# ").strip()
    title = doc.add_paragraph(style="paper title")
    title.alignment = WD_ALIGN_PARAGRAPH.CENTER
    run = title.add_run(title_text)
    set_font(run, 24)

    main = doc.add_section(WD_SECTION.CONTINUOUS)
    configure_section(main, 2)

    index = 1
    pending_caption: str | None = None
    in_abstract = False
    in_references = False
    while index < len(lines):
        raw = lines[index]
        stripped = raw.strip()
        if not stripped:
            index += 1
            continue
        if stripped == "## Abstract":
            in_abstract = True
            index += 1
            continue
        if stripped.startswith("**Keywords -**"):
            add_keywords(doc, stripped[len("**Keywords -**"):].strip())
            in_abstract = False
            index += 1
            continue
        if stripped.startswith("## "):
            heading = stripped[3:].strip()
            in_abstract = False
            if heading in {"ACKNOWLEDGMENT", "REFERENCES"}:
                add_component_heading(doc, heading)
                in_references = heading == "REFERENCES"
            else:
                add_heading1(doc, heading)
                in_references = False
            index += 1
            continue
        if stripped.startswith("### "):
            add_heading2(doc, stripped[4:].strip())
            index += 1
            continue
        if stripped == "[[FIGURE_PIPELINE]]":
            add_figure(doc)
            index += 1
            continue
        if stripped.startswith("**TABLE"):
            pending_caption = stripped.strip("*")
            index += 1
            continue
        if stripped.startswith("|"):
            rows, index = parse_table(lines, index)
            if pending_caption and rows:
                add_table(doc, pending_caption, rows)
            pending_caption = None
            continue
        if stripped == "\\[":
            equation: list[str] = []
            index += 1
            while index < len(lines) and lines[index].strip() != "\\]":
                equation.append(lines[index])
                index += 1
            add_equation(doc, equation)
            index += 1
            continue

        paragraph_lines = [stripped]
        index += 1
        while index < len(lines) and lines[index].strip():
            candidate = lines[index].strip()
            if candidate.startswith(("## ", "### ", "**TABLE", "|", "[[", "\\[", "**Keywords")):
                break
            paragraph_lines.append(candidate)
            index += 1
        paragraph = " ".join(paragraph_lines)
        if in_abstract:
            add_abstract(doc, paragraph)
            in_abstract = False
        elif in_references and re.match(r"^\[\d+\]", paragraph):
            p = doc.add_paragraph(style="references")
            if paragraph.startswith("[11]"):
                p.add_run().add_break(WD_BREAK.COLUMN)
            run = p.add_run(normalize_inline(paragraph))
            set_font(run, 8)
        else:
            add_body(doc, paragraph, size=8.5 if in_references else 10)

    props = doc.core_properties
    props.title = title_text
    props.subject = "Anonymous submission to ICAISG 2026"
    props.keywords = "large language model safety; retrieval-augmented generation; explainable artificial intelligence; AI governance; data provenance; auditability; document chunking; trustworthy artificial intelligence"
    props.language = "en-US"
    props.author = ""
    props.last_modified_by = ""
    props.comments = ""

    WORKING.parent.mkdir(parents=True, exist_ok=True)
    doc.save(WORKING)
    embed_svg_with_png_fallback(WORKING, SVG_FIGURE)
    print(WORKING)


if __name__ == "__main__":
    build()
