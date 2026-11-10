from __future__ import annotations

from pathlib import Path

from docx.enum.text import WD_ALIGN_PARAGRAPH
from docx.oxml.ns import qn
from docx.shared import Inches, Pt

import build_icaisg_paper as base


HERE = Path(__file__).resolve().parent
BASE_SET_FONT = base.set_font
BASE_CONFIGURE_STYLES = base.configure_styles
BASE_ADD_TABLE = base.add_table


def set_font_zh(run, size: float, *, bold=None, italic=None, small_caps=None) -> None:
    BASE_SET_FONT(run, size, bold=bold, italic=italic, small_caps=small_caps)
    run.font.name = "PingFang SC"
    fonts = run._element.get_or_add_rPr().get_or_add_rFonts()
    for slot in ("ascii", "hAnsi", "eastAsia", "cs"):
        fonts.set(qn(f"w:{slot}"), "PingFang SC")


def configure_styles_zh(doc) -> None:
    BASE_CONFIGURE_STYLES(doc)
    for name in (
        "Normal",
        "Body Text",
        "Heading 1",
        "Heading 2",
        "Heading 5",
        "paper title",
        "Abstract",
        "Keywords",
        "references",
    ):
        style = doc.styles[name]
        style.font.name = "PingFang SC"
        fonts = style.element.get_or_add_rPr().get_or_add_rFonts()
        for slot in ("ascii", "hAnsi", "eastAsia", "cs"):
            fonts.set(qn(f"w:{slot}"), "PingFang SC")


def add_abstract_zh(doc, text: str) -> None:
    paragraph = doc.add_paragraph(style="Abstract")
    label = paragraph.add_run("摘要—")
    set_font_zh(label, 9, bold=True)
    run = paragraph.add_run(base.normalize_inline(text))
    set_font_zh(run, 9, bold=True)


def add_keywords_zh(doc, text: str) -> None:
    paragraph = doc.add_paragraph(style="Keywords")
    label = paragraph.add_run("关键词—")
    set_font_zh(label, 9, bold=True)
    run = paragraph.add_run(base.normalize_inline(text))
    set_font_zh(run, 9, italic=True)


def add_component_heading_zh(doc, text: str) -> None:
    mapping = {"ACKNOWLEDGMENT": "致谢", "REFERENCES": "参考文献"}
    paragraph = doc.add_paragraph(style="Heading 1")
    paragraph.paragraph_format.keep_with_next = True
    run = paragraph.add_run(mapping.get(text, text))
    set_font_zh(run, 10)


def add_figure_zh(doc) -> None:
    base.create_pipeline_figure()
    paragraph = doc.add_paragraph()
    paragraph.alignment = WD_ALIGN_PARAGRAPH.CENTER
    paragraph.paragraph_format.space_before = Pt(3)
    paragraph.paragraph_format.space_after = Pt(1)
    run = paragraph.add_run()
    run.add_picture(str(base.FIGURE), width=Inches(3.45))
    doc_pr = run._element.xpath(".//wp:docPr")
    if doc_pr:
        doc_pr[0].set("descr", "SEAC 从带溯源信息的解析单元到可审计父块检索的处理流程")

    caption = doc.add_paragraph()
    caption.alignment = WD_ALIGN_PARAGRAPH.JUSTIFY
    caption.paragraph_format.first_line_indent = Inches(0)
    caption.paragraph_format.space_before = Pt(0)
    caption.paragraph_format.space_after = Pt(3)
    caption_run = caption.add_run(
        "图 1. SEAC 在边界评分、父块构建和子块触发检索过程中保留稳定的来源单元标识。"
    )
    set_font_zh(caption_run, 8)


def add_table_zh(doc, caption: str, rows: list[list[str]]) -> None:
    caption = caption.replace("TABLE I.", "表 I.")
    caption = caption.replace("TABLE II.", "表 II.")
    caption = caption.replace("TABLE III.", "表 III.")
    BASE_ADD_TABLE(doc, caption, rows)


def width_plan_zh(headers: list[str]) -> list[float]:
    plans = {
        ("划分", "项目数", "文档数", "单元数", "正则 token 数", "表格行数"): [
            0.64,
            0.46,
            0.46,
            0.54,
            0.78,
            0.62,
        ],
        ("方法", "单元 MRR", "Hit@5", "文本范围 MRR", "边界 F1"): [
            0.85,
            0.62,
            0.48,
            0.83,
            0.72,
        ],
        ("变体", "单元 MRR", "Hit@5", "相对完整模型变化"): [1.25, 0.62, 0.55, 1.08],
    }
    return plans.get(tuple(headers), [3.5 / len(headers)] * len(headers))


def main() -> None:
    base.SOURCE = HERE / "ICAISG2026_SEAC_blind_source_zh.md"
    base.WORKING = HERE / "review/ICAISG2026_SEAC_中文审阅稿.docx"
    base.set_font = set_font_zh
    base.configure_styles = configure_styles_zh
    base.add_abstract = add_abstract_zh
    base.add_keywords = add_keywords_zh
    base.add_component_heading = add_component_heading_zh
    base.add_figure = add_figure_zh
    base.add_table = add_table_zh
    base.width_plan = width_plan_zh
    base.build()


if __name__ == "__main__":
    main()
