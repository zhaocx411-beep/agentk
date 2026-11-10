#!/usr/bin/env python3
"""Build signed-pending and anonymous ChinaSoft 2026 IISE manuscripts."""

from __future__ import annotations

import argparse
import json
import re
import zipfile
from pathlib import Path

from docx import Document
from docx.enum.section import WD_SECTION_START
from docx.enum.table import WD_CELL_VERTICAL_ALIGNMENT, WD_TABLE_ALIGNMENT
from docx.enum.text import WD_ALIGN_PARAGRAPH, WD_BREAK, WD_LINE_SPACING, WD_TAB_ALIGNMENT
from docx.oxml import OxmlElement
from docx.oxml.ns import qn
from docx.shared import Mm, Pt, RGBColor


ROOT = Path(__file__).resolve().parent
SOURCE = ROOT / "paper_zh.md"
AUTHORS = ROOT / "author_metadata.json"
SIGNED_OUTPUT = ROOT / "SEAC_ChinaSoft2026_IISE_署名待填.docx"
ANON_OUTPUT = ROOT / "SEAC_ChinaSoft2026_IISE_匿名备份.docx"

FONT_CN = "Songti SC"
FONT_HEI = "Heiti SC"
FONT_FANG = "STFangsong"
FONT_EN = "Times New Roman"
FONT_MATH = "Cambria Math"


def set_cell_shading(cell, fill: str) -> None:
    tc_pr = cell._tc.get_or_add_tcPr()
    shading = tc_pr.find(qn("w:shd"))
    if shading is None:
        shading = OxmlElement("w:shd")
        tc_pr.append(shading)
    shading.set(qn("w:fill"), fill)


def set_cell_margins(cell, top=55, start=65, bottom=55, end=65) -> None:
    tc = cell._tc
    tc_pr = tc.get_or_add_tcPr()
    tc_mar = tc_pr.first_child_found_in("w:tcMar")
    if tc_mar is None:
        tc_mar = OxmlElement("w:tcMar")
        tc_pr.append(tc_mar)
    for edge, value in (("top", top), ("start", start), ("bottom", bottom), ("end", end)):
        tag = "w:" + edge
        node = tc_mar.find(qn(tag))
        if node is None:
            node = OxmlElement(tag)
            tc_mar.append(node)
        node.set(qn("w:w"), str(value))
        node.set(qn("w:type"), "dxa")


def set_cell_width(cell, twips: int) -> None:
    tc_pr = cell._tc.get_or_add_tcPr()
    tc_w = tc_pr.find(qn("w:tcW"))
    if tc_w is None:
        tc_w = OxmlElement("w:tcW")
        tc_pr.append(tc_w)
    tc_w.set(qn("w:w"), str(twips))
    tc_w.set(qn("w:type"), "dxa")


def set_table_geometry(table, widths: list[int]) -> None:
    """Write a fixed OOXML grid so Word and LibreOffice use the same widths."""
    total = sum(widths)
    tbl_pr = table._tbl.tblPr
    tbl_w = tbl_pr.find(qn("w:tblW"))
    if tbl_w is None:
        tbl_w = OxmlElement("w:tblW")
        tbl_pr.append(tbl_w)
    tbl_w.set(qn("w:w"), str(total))
    tbl_w.set(qn("w:type"), "dxa")
    tbl_ind = tbl_pr.find(qn("w:tblInd"))
    if tbl_ind is None:
        tbl_ind = OxmlElement("w:tblInd")
        tbl_pr.append(tbl_ind)
    # Match the start-cell inset so the visible border aligns consistently in
    # Word even when a viewer does not fully honor centered-table semantics.
    tbl_ind.set(qn("w:w"), "65")
    tbl_ind.set(qn("w:type"), "dxa")
    layout = tbl_pr.find(qn("w:tblLayout"))
    if layout is None:
        layout = OxmlElement("w:tblLayout")
        tbl_pr.append(layout)
    layout.set(qn("w:type"), "fixed")
    grid = table._tbl.tblGrid
    for child in list(grid):
        grid.remove(child)
    for width in widths:
        column = OxmlElement("w:gridCol")
        column.set(qn("w:w"), str(width))
        grid.append(column)


def set_repeat_header(row) -> None:
    tr_pr = row._tr.get_or_add_trPr()
    tbl_header = OxmlElement("w:tblHeader")
    tbl_header.set(qn("w:val"), "true")
    tr_pr.append(tbl_header)


def prevent_row_split(row) -> None:
    tr_pr = row._tr.get_or_add_trPr()
    cant_split = OxmlElement("w:cantSplit")
    tr_pr.append(cant_split)


def set_table_borders(table, inner: bool = True) -> None:
    tbl_pr = table._tbl.tblPr
    borders = tbl_pr.find(qn("w:tblBorders"))
    if borders is None:
        borders = OxmlElement("w:tblBorders")
        tbl_pr.append(borders)
    for name in ("top", "left", "bottom", "right", "insideH", "insideV"):
        edge = borders.find(qn(f"w:{name}"))
        if edge is None:
            edge = OxmlElement(f"w:{name}")
            borders.append(edge)
        edge.set(qn("w:val"), "single" if inner or name in {"top", "bottom"} else "nil")
        edge.set(qn("w:sz"), "5" if name in {"top", "bottom"} else "3")
        edge.set(qn("w:space"), "0")
        edge.set(qn("w:color"), "666666")


def set_east_asia_font(run, east_asia: str, latin: str | None = None) -> None:
    latin = latin or east_asia
    run.font.name = latin
    r_pr = run._element.get_or_add_rPr()
    r_fonts = r_pr.get_or_add_rFonts()
    r_fonts.set(qn("w:ascii"), latin)
    r_fonts.set(qn("w:hAnsi"), latin)
    r_fonts.set(qn("w:eastAsia"), east_asia)
    r_fonts.set(qn("w:cs"), latin)
    lang = r_pr.find(qn("w:lang"))
    if lang is None:
        lang = OxmlElement("w:lang")
        r_pr.append(lang)
    lang.set(qn("w:val"), "en-US")
    lang.set(qn("w:eastAsia"), "zh-CN")


def contains_cjk(text: str) -> bool:
    return any("\u3400" <= char <= "\u9fff" for char in text)


def style_run(run, size: float, *, bold=False, italic=False, font_cn=FONT_CN, font_en=FONT_EN) -> None:
    # LibreOffice on macOS can ignore w:eastAsia for a mixed-script run.  A CJK
    # run therefore names the Chinese font in every font slot; Latin-only runs
    # retain Times New Roman.
    set_east_asia_font(run, font_cn, font_cn if contains_cjk(run.text) else font_en)
    run.font.size = Pt(size)
    run.bold = bold
    run.italic = italic


def add_mixed_text(
    paragraph,
    text: str,
    size: float,
    *,
    bold=False,
    italic=False,
    placeholder_red=False,
    font_cn=FONT_CN,
    font_en=FONT_EN,
):
    parts: list[str] = []
    current = ""
    current_cjk: bool | None = None
    for char in text:
        char_cjk = "\u3400" <= char <= "\u9fff"
        if current and char_cjk != current_cjk:
            parts.append(current)
            current = char
        else:
            current += char
        current_cjk = char_cjk
    if current:
        parts.append(current)
    last_run = None
    red = placeholder_red and "[" in text and ("待确认" in text or "TO CONFIRM" in text)
    for part in parts or [""]:
        run = paragraph.add_run(part)
        style_run(run, size, bold=bold, italic=italic, font_cn=font_cn, font_en=font_en)
        if red:
            run.font.color.rgb = RGBColor(192, 0, 0)
        last_run = run
    return last_run


def add_field(paragraph, instruction: str) -> None:
    run = paragraph.add_run()
    begin = OxmlElement("w:fldChar")
    begin.set(qn("w:fldCharType"), "begin")
    instr = OxmlElement("w:instrText")
    instr.set(qn("xml:space"), "preserve")
    instr.text = instruction
    separate = OxmlElement("w:fldChar")
    separate.set(qn("w:fldCharType"), "separate")
    end = OxmlElement("w:fldChar")
    end.set(qn("w:fldCharType"), "end")
    run._r.extend([begin, instr, separate, end])


def set_columns(section, count: int, space_twips: int = 400) -> None:
    sect_pr = section._sectPr
    cols = sect_pr.find(qn("w:cols"))
    if cols is None:
        cols = OxmlElement("w:cols")
        sect_pr.append(cols)
    cols.set(qn("w:num"), str(count))
    cols.set(qn("w:space"), str(space_twips))
    if count == 1:
        cols.attrib.pop(qn("w:sep"), None)


def configure_section(section, columns: int) -> None:
    section.page_width = Mm(210)
    section.page_height = Mm(297)
    section.top_margin = Mm(25.8)
    section.bottom_margin = Mm(25.8)
    section.left_margin = Mm(20.0)
    section.right_margin = Mm(20.0)
    section.header_distance = Mm(10)
    section.footer_distance = Mm(10)
    set_columns(section, columns, 400)


def add_header(section, anonymous: bool, *, linked: bool = False) -> None:
    section.different_first_page_header_footer = not linked
    header = section.header
    header.is_linked_to_previous = linked
    if linked:
        return
    p = header.paragraphs[0]
    p.alignment = WD_ALIGN_PARAGRAPH.RIGHT
    p.paragraph_format.space_after = Pt(0)
    label = "匿名投稿稿  " if anonymous else "ChinaSoft 2026 专题投稿稿  "
    add_mixed_text(p, label, 8)
    add_field(p, "PAGE")
    p_pr = p._p.get_or_add_pPr()
    p_bdr = OxmlElement("w:pBdr")
    bottom = OxmlElement("w:bottom")
    bottom.set(qn("w:val"), "single")
    bottom.set(qn("w:sz"), "6")
    bottom.set(qn("w:space"), "2")
    bottom.set(qn("w:color"), "444444")
    p_bdr.append(bottom)
    p_pr.append(p_bdr)


def configure_styles(document: Document) -> None:
    styles = document.styles
    normal = styles["Normal"]
    normal.font.name = FONT_EN
    normal.font.size = Pt(10.5)
    r_fonts = normal.element.rPr.rFonts
    r_fonts.set(qn("w:ascii"), FONT_EN)
    r_fonts.set(qn("w:hAnsi"), FONT_EN)
    r_fonts.set(qn("w:eastAsia"), FONT_CN)
    normal.paragraph_format.alignment = WD_ALIGN_PARAGRAPH.JUSTIFY
    normal.paragraph_format.line_spacing_rule = WD_LINE_SPACING.EXACTLY
    normal.paragraph_format.line_spacing = Pt(14.2)
    normal.paragraph_format.first_line_indent = Pt(21)
    normal.paragraph_format.space_before = Pt(0)
    normal.paragraph_format.space_after = Pt(0)

    for name, size, font, before, after in (
        ("Heading 1", 14, FONT_HEI, 8, 4),
        ("Heading 2", 10.5, FONT_HEI, 5, 2),
        ("Heading 3", 10.5, FONT_CN, 3, 1),
    ):
        style = styles[name]
        style.font.name = FONT_EN
        style.font.size = Pt(size)
        style.font.bold = name != "Heading 3"
        style.font.color.rgb = RGBColor(0, 0, 0)
        style.element.rPr.rFonts.set(qn("w:ascii"), FONT_EN)
        style.element.rPr.rFonts.set(qn("w:hAnsi"), FONT_EN)
        style.element.rPr.rFonts.set(qn("w:eastAsia"), font)
        style.paragraph_format.first_line_indent = Pt(0)
        style.paragraph_format.space_before = Pt(before)
        style.paragraph_format.space_after = Pt(after)
        style.paragraph_format.keep_with_next = True
        style.paragraph_format.keep_together = True
        style.paragraph_format.line_spacing = 1


def parse_source() -> tuple[dict[str, str], list[str]]:
    text = SOURCE.read_text(encoding="utf-8")
    front, body = text.split("---", 2)[1:]
    meta: dict[str, str] = {}
    for line in front.strip().splitlines():
        if ":" in line:
            key, value = line.split(":", 1)
            meta[key.strip()] = value.strip()
    return meta, body.strip().splitlines()


def section_single_paragraph(lines: list[str], heading: str) -> str:
    marker = f"# {heading}"
    start = lines.index(marker) + 1
    collected = []
    for line in lines[start:]:
        if line.startswith("# "):
            break
        if line.strip():
            collected.append(line.strip())
    return " ".join(collected)


def add_labeled_paragraph(document, label: str, text: str, size: float, english=False) -> None:
    p = document.add_paragraph()
    p.alignment = WD_ALIGN_PARAGRAPH.JUSTIFY
    p.paragraph_format.first_line_indent = Pt(0)
    p.paragraph_format.space_before = Pt(2)
    p.paragraph_format.space_after = Pt(1.5)
    p.paragraph_format.line_spacing_rule = WD_LINE_SPACING.EXACTLY
    p.paragraph_format.line_spacing = Pt(13.0 if english else 12.0)
    label_run = p.add_run(label)
    style_run(label_run, size, bold=True, font_cn=FONT_HEI if not english else FONT_CN)
    text_run = p.add_run(text)
    style_run(text_run, size)


def add_front_matter(document: Document, meta: dict[str, str], author_meta: dict, anonymous: bool) -> None:
    p = document.add_paragraph()
    p.alignment = WD_ALIGN_PARAGRAPH.CENTER
    p.paragraph_format.space_before = Pt(2)
    p.paragraph_format.space_after = Pt(6)
    p.paragraph_format.line_spacing_rule = WD_LINE_SPACING.EXACTLY
    p.paragraph_format.line_spacing = Pt(30)
    p.paragraph_format.keep_with_next = True
    style_run(p.add_run(meta["title_zh"]), 22, bold=True, font_cn=FONT_HEI)

    p = document.add_paragraph()
    p.alignment = WD_ALIGN_PARAGRAPH.CENTER
    p.paragraph_format.space_after = Pt(3)
    p.paragraph_format.keep_with_next = True
    authors_zh = "作者信息因评审要求隐去" if anonymous else "    ".join(author_meta["authors_zh"])
    style_run(p.add_run(authors_zh), 16, font_cn=FONT_FANG)
    if not anonymous and "待确认" in authors_zh:
        p.runs[-1].font.color.rgb = RGBColor(192, 0, 0)

    if not anonymous:
        for affiliation in author_meta["affiliations_zh"]:
            p = document.add_paragraph()
            p.alignment = WD_ALIGN_PARAGRAPH.CENTER
            p.paragraph_format.space_after = Pt(0)
            p.paragraph_format.keep_with_next = True
            add_mixed_text(p, affiliation, 7.5, placeholder_red=True)

    abstract_zh = section_single_paragraph(parse_source()[1], "中文摘要")
    add_labeled_paragraph(document, "摘  要  ", abstract_zh, 9)
    add_labeled_paragraph(document, "关键词  ", meta["keywords_zh"], 9)
    add_labeled_paragraph(document, "中图法分类号  ", meta["category"] + "    DOI号：投稿时不提供", 9)

    p = document.add_paragraph()
    p.alignment = WD_ALIGN_PARAGRAPH.CENTER
    p.paragraph_format.space_before = Pt(5)
    p.paragraph_format.space_after = Pt(3)
    p.paragraph_format.keep_with_next = True
    style_run(p.add_run(meta["title_en"]), 14, bold=True)

    p = document.add_paragraph()
    p.alignment = WD_ALIGN_PARAGRAPH.CENTER
    p.paragraph_format.space_after = Pt(1)
    names_en = "AUTHOR INFORMATION WITHHELD FOR REVIEW" if anonymous else "    ".join(author_meta["authors_en"])
    add_mixed_text(p, names_en, 10.5, placeholder_red=not anonymous)
    if not anonymous:
        for affiliation in author_meta["affiliations_en"]:
            p = document.add_paragraph()
            p.alignment = WD_ALIGN_PARAGRAPH.CENTER
            p.paragraph_format.space_after = Pt(0)
            add_mixed_text(p, affiliation, 7.5, placeholder_red=True)

    abstract_en = section_single_paragraph(parse_source()[1], "English abstract")
    add_labeled_paragraph(document, "Abstract  ", abstract_en, 10.5, english=True)
    add_labeled_paragraph(document, "Key words  ", meta["keywords_en"], 10.5, english=True)

    if not anonymous:
        p = document.add_paragraph()
        p.paragraph_format.space_before = Pt(4)
        p.paragraph_format.space_after = Pt(2)
        p.paragraph_format.first_line_indent = Pt(0)
        p_pr = p._p.get_or_add_pPr()
        p_bdr = OxmlElement("w:pBdr")
        top = OxmlElement("w:top")
        top.set(qn("w:val"), "single")
        top.set(qn("w:sz"), "4")
        top.set(qn("w:space"), "3")
        top.set(qn("w:color"), "777777")
        p_bdr.append(top)
        p_pr.append(p_bdr)
        contact = (
            f"通讯作者：{author_meta['corresponding_author_zh']}；E-mail：{author_meta['email']}；"
            f"手机：{author_meta['phone']}；联系地址：{author_meta['address']}。"
        )
        add_mixed_text(p, contact, 7.5, placeholder_red=True)
        p2 = document.add_paragraph()
        p2.paragraph_format.first_line_indent = Pt(0)
        p2.paragraph_format.space_after = Pt(2)
        add_mixed_text(p2, "基金项目：" + author_meta["funding"], 7.5, placeholder_red=True)


def add_body_paragraph(document: Document, text: str) -> None:
    p = document.add_paragraph()
    p.alignment = WD_ALIGN_PARAGRAPH.JUSTIFY
    p.paragraph_format.first_line_indent = Pt(21)
    p.paragraph_format.line_spacing_rule = WD_LINE_SPACING.EXACTLY
    p.paragraph_format.line_spacing = Pt(14.2)
    p.paragraph_format.space_after = Pt(0)
    add_mixed_text(p, text, 10.5)


def add_heading(document: Document, text: str, level: int) -> None:
    style = "Heading 1" if level == 1 else "Heading 2" if level == 2 else "Heading 3"
    p = document.add_paragraph(style=style)
    if level == 1:
        p.alignment = WD_ALIGN_PARAGRAPH.LEFT
    add_mixed_text(
        p,
        text,
        14 if level == 1 else 10.5,
        bold=level < 3,
        placeholder_red=False,
        font_cn=FONT_HEI if level < 3 else FONT_CN,
    )


def markdown_rows(lines: list[str]) -> list[list[str]]:
    rows: list[list[str]] = []
    for i, line in enumerate(lines):
        values = [value.strip() for value in line.strip().strip("|").split("|")]
        if i == 1 and all(re.fullmatch(r":?-{3,}:?", value) for value in values):
            continue
        rows.append(values)
    return rows


def table_widths(title: str, cols: int, full_width: bool) -> list[int]:
    total = 9600 if full_width else 4450
    if "表A1" in title:
        return [700, 1250, 750, 1000, 1000, 1700, 950, 2250]
    if "表A2" in title:
        return [1100, 1450, 1450, 2500, 3100]
    if "表A3" in title:
        return [2300, 1300, 1300, 1300, 3400]
    if "表A4" in title:
        return [2100, 1150, 1050, 1050, 1050, 1500, 1700]
    if "表1" in title:
        return [1450, 1050, 1200, 1650, 1500, 2750]
    if "表2" in title:
        return [1050, 1050, 1250, 2800, 2250]
    if "表3" in title:
        return [1850, 3350, 4400]
    if "表4" in title:
        return [2200, 1300, 1500, 1200, 1200, 2200]
    if "表5" in title:
        return [1200, 1200, 2100, 2600, 1500]
    if "表6" in title:
        return [2300, 900, 1900, 1600, 1900]
    if "表7" in title:
        return [2500, 1300, 1500, 1300, 3000]
    if "表8" in title:
        return [1800, 1500, 1900, 1800, 2600]
    q, r = divmod(total, cols)
    return [q + (1 if i < r else 0) for i in range(cols)]


def begin_full_width(document: Document) -> None:
    section = document.add_section(WD_SECTION_START.CONTINUOUS)
    configure_section(section, 1)
    add_header(section, False, linked=True)


def end_full_width(document: Document) -> None:
    section = document.add_section(WD_SECTION_START.CONTINUOUS)
    configure_section(section, 2)
    add_header(section, False, linked=True)


def add_table(document: Document, title: str, rows: list[list[str]], anonymous: bool) -> None:
    # Three-column parameter/result tables also need the full text width in
    # the CJC two-column layout; keeping them in one column clips long cells.
    full_width = len(rows[0]) >= 3
    if full_width:
        begin_full_width(document)
    caption = document.add_paragraph()
    caption.alignment = WD_ALIGN_PARAGRAPH.CENTER
    caption.paragraph_format.space_before = Pt(3)
    caption.paragraph_format.space_after = Pt(2)
    caption.paragraph_format.keep_with_next = True
    style_run(caption.add_run(title), 9, bold=True, font_cn=FONT_HEI)

    table = document.add_table(rows=len(rows), cols=len(rows[0]))
    table.alignment = WD_TABLE_ALIGNMENT.CENTER
    table.autofit = False
    set_table_borders(table)
    widths = table_widths(title, len(rows[0]), full_width)
    set_table_geometry(table, widths)
    for r_idx, row in enumerate(table.rows):
        prevent_row_split(row)
        if r_idx == 0:
            set_repeat_header(row)
        for c_idx, cell in enumerate(row.cells):
            set_cell_width(cell, widths[c_idx])
            set_cell_margins(cell)
            cell.vertical_alignment = WD_CELL_VERTICAL_ALIGNMENT.CENTER
            if r_idx == 0:
                set_cell_shading(cell, "E7E7E7")
            p = cell.paragraphs[0]
            p.alignment = WD_ALIGN_PARAGRAPH.LEFT if c_idx == 0 else WD_ALIGN_PARAGRAPH.CENTER
            p.paragraph_format.first_line_indent = Pt(0)
            p.paragraph_format.space_before = Pt(0)
            p.paragraph_format.space_after = Pt(0)
            p.paragraph_format.line_spacing = 1
            add_mixed_text(p, rows[r_idx][c_idx], 7.6, bold=r_idx == 0)
    spacer = document.add_paragraph()
    spacer.paragraph_format.space_after = Pt(0)
    spacer.paragraph_format.line_spacing = Pt(2)
    if full_width:
        end_full_width(document)


def add_equation(document: Document, marker: str) -> None:
    match = re.fullmatch(r"\[\[EQUATION:(.+)\|(\d+)\]\]", marker)
    if not match:
        raise ValueError(f"Bad equation marker: {marker}")
    raw, number = match.groups()
    mapping = {
        r"D=(u_1,u_2,\ldots,u_n).": "D = (u₁, u₂, …, uₙ).",
        r"t(r_j)=\operatorname{header}(h)\Vert\operatorname{row}(r_j).": "t(rⱼ) = header(h) ∥ row(rⱼ).",
        r"\rho(u)=\frac{\sum_{v\in V}\operatorname{count}(v,u)}{\max(1,\operatorname{tokens}(u))}.": "ρ(u) = Σ[v∈V] count(v,u) / max(1,tokens(u)).",
        r"d_i=1-\cos(e_i,e_{i+1}).": "dᵢ = 1 − cos(eᵢ, eᵢ₊₁).",
        r"s_i=\min\{1,I_h+0.7I_{path}+0.5I_{group}\}.": "sᵢ = min{1, Iₕ + 0.7Ipath + 0.5Igroup}.",
        r"k_i=\min\{1,10|\rho(u_i)-\rho(u_{i+1})|\}.": "kᵢ = min{1, 10|ρ(uᵢ) − ρ(uᵢ₊₁)|}.",
        r"p_i=0.35I_{same\_path}+1.0I_{same\_group}.": "pᵢ = 0.35Isame-path + 1.0Isame-group.",
        r"b_i=0.45d_i+0.30s_i+0.25k_i-p_i.": "bᵢ = 0.45dᵢ + 0.30sᵢ + 0.25kᵢ − pᵢ.",
        r"\theta_D=Q_{0.8}(b)+0.08r_t-0.05r_h.": "θ_D = Q₀.₈(b) + 0.08rₜ − 0.05rₕ.",
        r"S(p,q)=0.35S_p(q)+0.65\max_{c\in children(p)}S_c(q).": "S(p,q) = 0.35Sₚ(q) + 0.65 max S꜀(q).",
    }
    equation = mapping.get(raw, raw)
    p = document.add_paragraph()
    p.paragraph_format.first_line_indent = Pt(0)
    p.paragraph_format.space_before = Pt(2)
    p.paragraph_format.space_after = Pt(2)
    p.paragraph_format.line_spacing = 1
    p.paragraph_format.keep_together = True
    p.paragraph_format.tab_stops.add_tab_stop(Mm(38), WD_TAB_ALIGNMENT.CENTER)
    p.paragraph_format.tab_stops.add_tab_stop(Mm(78), WD_TAB_ALIGNMENT.RIGHT)
    p.add_run("\t")
    run = p.add_run(equation)
    size = 8.4 if number in {"3", "5", "10"} else 9.5
    style_run(run, size, font_cn=FONT_CN, font_en=FONT_MATH)
    p.add_run("\t")
    number_run = p.add_run(f"（{number}）")
    style_run(number_run, 9, font_cn=FONT_CN, font_en=FONT_EN)


def add_pipeline_figure(document: Document, anonymous: bool) -> None:
    begin_full_width(document)
    table = document.add_table(rows=1, cols=9)
    table.alignment = WD_TABLE_ALIGNMENT.CENTER
    table.autofit = False
    figure_widths = [1700 if i % 2 == 0 else 275 for i in range(9)]
    set_table_geometry(table, figure_widths)
    boxes = [
        "工业文档\n标题·段落·表格",
        "可追溯单元\n块/单元/标题路径",
        "父块与子块\n结构保护·行级索引",
        "稠密检索\n子块激活父块",
        "证据输出\n上下文+精确来源",
    ]
    for i in range(9):
        cell = table.cell(0, i)
        set_cell_width(cell, figure_widths[i])
        set_cell_margins(cell, 90, 65, 90, 65)
        cell.vertical_alignment = WD_CELL_VERTICAL_ALIGNMENT.CENTER
        p = cell.paragraphs[0]
        p.alignment = WD_ALIGN_PARAGRAPH.CENTER
        p.paragraph_format.first_line_indent = Pt(0)
        p.paragraph_format.space_after = Pt(0)
        if i % 2 == 0:
            set_cell_shading(cell, "E6EDF3" if i in {2, 6} else "F2F2F2")
            add_mixed_text(p, boxes[i // 2], 8.5, bold=True)
        else:
            add_mixed_text(p, "→", 14, bold=True)
    set_table_borders(table, inner=False)
    caption = document.add_paragraph()
    caption.alignment = WD_ALIGN_PARAGRAPH.CENTER
    caption.paragraph_format.space_before = Pt(2)
    caption.paragraph_format.space_after = Pt(2)
    caption.paragraph_format.keep_together = True
    add_mixed_text(caption, "图1  SEAC 从工业文档到可追溯父块证据的构造与检索流程", 9)
    end_full_width(document)


def add_reference(document: Document, text: str) -> None:
    p = document.add_paragraph()
    p.paragraph_format.left_indent = Pt(12)
    p.paragraph_format.first_line_indent = Pt(-12)
    p.paragraph_format.space_after = Pt(1)
    p.paragraph_format.line_spacing_rule = WD_LINE_SPACING.EXACTLY
    p.paragraph_format.line_spacing = Pt(10)
    add_mixed_text(p, text, 7.5)


def add_body(document: Document, lines: list[str], author_meta: dict, anonymous: bool) -> None:
    # Skip front-matter abstract sections; start at the numbered introduction.
    start = lines.index("# 1 引言")
    i = start
    in_references = False
    while i < len(lines):
        line = lines[i].strip()
        if not line:
            i += 1
            continue
        if line.startswith("[[TABLE:"):
            title = line[len("[[TABLE:"):-2]
            table_lines = []
            i += 1
            while i < len(lines) and lines[i].strip().startswith("|"):
                table_lines.append(lines[i].strip())
                i += 1
            add_table(document, title, markdown_rows(table_lines), anonymous)
            continue
        if line == "[[FIGURE:PIPELINE]]":
            add_pipeline_figure(document, anonymous)
            i += 1
            continue
        if line.startswith("[[EQUATION:"):
            add_equation(document, line)
            i += 1
            continue
        if line.startswith("# "):
            title = line[2:].strip()
            if title in {"作者简介（待确认）", "Background"}:
                if title == "作者简介（待确认）":
                    add_heading(document, "作者简介", 1)
                    bios = ["作者信息按评审要求隐去。"] if anonymous else author_meta["author_bios"]
                    for bio in bios:
                        add_body_paragraph(document, bio)
                    i += 1
                    while i < len(lines) and not lines[i].startswith("# "):
                        i += 1
                    continue
            add_heading(document, title, 1)
            in_references = title == "参考文献"
            i += 1
            continue
        if line.startswith("## "):
            add_heading(document, line[3:].strip(), 2)
            i += 1
            continue
        if in_references and re.match(r"^\[\d+\]", line):
            add_reference(document, line)
            i += 1
            continue
        add_body_paragraph(document, line)
        i += 1


def set_update_fields(document: Document) -> None:
    settings = document.settings.element
    update = settings.find(qn("w:updateFields"))
    if update is None:
        update = OxmlElement("w:updateFields")
        settings.append(update)
    update.set(qn("w:val"), "true")


def scrub_package(path: Path, anonymous: bool) -> None:
    with zipfile.ZipFile(path, "r") as src:
        parts = {name: src.read(name) for name in src.namelist()}
    for name, payload in list(parts.items()):
        if name == "docProps/custom.xml":
            del parts[name]
            continue
        if name.startswith("docProps/") and name.endswith(".xml"):
            text = payload.decode("utf-8", errors="ignore")
            text = re.sub(r"<dc:creator>.*?</dc:creator>", "<dc:creator></dc:creator>", text)
            text = re.sub(r"<cp:lastModifiedBy>.*?</cp:lastModifiedBy>", "<cp:lastModifiedBy></cp:lastModifiedBy>", text)
            text = re.sub(r"<Company>.*?</Company>", "<Company></Company>", text)
            parts[name] = text.encode("utf-8")
        elif name.endswith(".xml"):
            # Revision-session identifiers can disclose document editing
            # history.  They are layout-neutral and unnecessary for a clean
            # external submission package.
            parts[name] = re.sub(rb'\s+w:rsid[A-Za-z]*="[^"]*"', b"", payload)
    temp = path.with_suffix(".scrubbed.docx")
    with zipfile.ZipFile(temp, "w", compression=zipfile.ZIP_DEFLATED) as out:
        for name, payload in parts.items():
            out.writestr(name, payload)
    temp.replace(path)


def build(output: Path, anonymous: bool) -> Path:
    meta, lines = parse_source()
    author_meta = json.loads(AUTHORS.read_text(encoding="utf-8"))
    document = Document()
    configure_styles(document)
    first = document.sections[0]
    configure_section(first, 1)
    add_header(first, anonymous, linked=False)
    add_front_matter(document, meta, author_meta, anonymous)

    body_section = document.add_section(WD_SECTION_START.CONTINUOUS)
    configure_section(body_section, 2)
    add_header(body_section, anonymous, linked=True)
    add_body(document, lines, author_meta, anonymous)

    for section in document.sections:
        configure_section(section, int(section._sectPr.find(qn("w:cols")).get(qn("w:num"), "1")))
    set_update_fields(document)
    document.core_properties.title = meta["title_zh"]
    document.core_properties.subject = meta["track"]
    document.core_properties.author = "" if anonymous else "Authors pending confirmation"
    document.core_properties.last_modified_by = ""
    document.core_properties.keywords = meta["keywords_zh"]
    document.core_properties.comments = ""
    output.parent.mkdir(parents=True, exist_ok=True)
    document.save(output)
    scrub_package(output, anonymous)
    return output


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--mode", choices=("both", "signed", "anonymous"), default="both")
    args = parser.parse_args()
    if args.mode in {"both", "signed"}:
        print(build(SIGNED_OUTPUT, anonymous=False))
    if args.mode in {"both", "anonymous"}:
        print(build(ANON_OUTPUT, anonymous=True))


if __name__ == "__main__":
    main()
