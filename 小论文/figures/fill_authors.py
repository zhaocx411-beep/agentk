"""
把作者信息填进投稿修订版，生成最终投稿版 docx。

会务已确认：投稿需提交含作者信息的版本，送审前由会务自行删除作者信息。

作者块在模板里不是普通段落，而是夹在两个分节符之间的独立多栏区域：
标题占满整页宽 -> 作者块按作者人数分栏 -> 正文切回双栏。
因此本脚本做两件事：
  1. 把作者块所在节的栏数由模板预设的 4 改为实际作者数 3；
  2. 在该节的正文段里写入三位作者，作者之间用显式分栏符隔开，
     保证每位作者独占一栏，不依赖 Word 的自动栏平衡（各作者行数不等时会错位）。

通讯作者的标注方式照抄官方 template.docx：姓名后加星号，
并在其邮箱行正下方另起一行写 "*Corresponding author"。

输入：ICAISG_2026_SEAC_投稿修订版.docx
输出：ICAISG_2026_SEAC_投稿版.docx
"""

import re
from pathlib import Path

import docx
from docx.enum.text import WD_ALIGN_PARAGRAPH, WD_BREAK
from docx.oxml.ns import qn

ROOT = Path(__file__).resolve().parent.parent
SRC = ROOT / "ICAISG_2026_SEAC_投稿修订版.docx"
DST = ROOT / "ICAISG_2026_SEAC_投稿版.docx"

DEPT = "School of Electronic Engineering"
ORG = "Beijing University of Posts and Telecommunications"
CITY = "Beijing, China"

# 每个元素是一栏，按署名顺序排列。仅通讯作者提供了邮箱，
# 其余两位暂缺，故不写第 5 行（IEEE 允许只列通讯作者邮箱）。
AUTHOR_COLUMNS = [
    ["Chenxu Zhao*", DEPT, ORG, CITY, "2024110375@bupt.cn", "*Corresponding author"],
    ["Xiai Wang", DEPT, ORG, CITY],
    ["Yongjun Zhang", DEPT, ORG, CITY],
]

TITLE = (
    "Provenance-Preserving Structure-Aware Chunking for "
    "Auditable Enterprise Retrieval-Augmented Generation"
)


def set_author_section_columns(doc, n_cols):
    """作者块所在节的栏数改为实际作者数。该 sectPr 挂在作者块最后一段上。"""
    for p in doc.paragraphs[:6]:
        sect = p._p.find(qn("w:pPr") + "/" + qn("w:sectPr"))
        if sect is None:
            pPr = p._p.find(qn("w:pPr"))
            sect = pPr.find(qn("w:sectPr")) if pPr is not None else None
        if sect is None:
            continue
        cols = sect.find(qn("w:cols"))
        if cols is not None and cols.get(qn("w:num")) == "4":
            cols.set(qn("w:num"), str(n_cols))
            return True
    return False


def write_author_block(para, columns):
    """把各栏作者写进同一段：栏内用软换行，栏间用分栏符。"""
    # 清空原有 run（原稿此处为空段，仅保险起见）
    for r in list(para.runs):
        r._r.getparent().remove(r._r)

    # 作者块居中；原段落继承的两端对齐会把姓名行拉散
    para.alignment = WD_ALIGN_PARAGRAPH.CENTER

    for col_idx, lines in enumerate(columns):
        run = para.add_run()
        if col_idx:
            run.add_break(WD_BREAK.COLUMN)
        for line_idx, line in enumerate(lines):
            if line_idx:
                run.add_break()
            run.add_text(line)
        run.font.name = "Times New Roman"
        run._element.rPr.rFonts.set(qn("w:eastAsia"), "Times New Roman")


def main():
    doc = docx.Document(str(SRC))

    assert set_author_section_columns(doc, len(AUTHOR_COLUMNS)), "作者块分节符定位失败"

    # 作者块正文段：标题之后、Abstract 之前，样式为 Author 且当前为空
    target = next(
        p
        for p in doc.paragraphs[1:5]
        if p.style.name == "Author" and not p.text.strip()
        and p._p.find(qn("w:pPr") + "/" + qn("w:sectPr")) is None
    )
    write_author_block(target, AUTHOR_COLUMNS)

    # PDF eXpress 会读取文档属性，与正文保持一致
    doc.core_properties.title = TITLE
    doc.core_properties.author = "Chenxu Zhao; Xiai Wang; Yongjun Zhang"

    doc.save(str(DST))
    print("saved", DST)

    out = docx.Document(str(DST))
    xml = out.paragraphs[:5]
    for i, p in enumerate(xml):
        if p.text.strip():
            print(f"--- para {i} ({p.style.name}) ---")
            print(p.text)
    body_xml = out.element.body.xml
    print("column breaks:", body_xml.count('w:type="column"'))
    print("author section cols:", re.findall(r'<w:cols[^>]*w:num="(\d+)"', body_xml))


if __name__ == "__main__":
    main()
