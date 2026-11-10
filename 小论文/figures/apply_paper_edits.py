"""
在不改动原稿的前提下，生成投稿修订版 docx。

改动共两处，均为增量插入，不删除任何既有文字，也不改动版式：
  1. Related Work 末尾新增一段，把 SEAC 定位到 RAG 取证 / 责任归因这条安全线索上，
     引用新增的 [18]-[21]。这一段不可省略：IEEE 不接受未被正文引用的参考文献条目；
  2. 参考文献表末尾追加 4 条 2024-2026 年文献。

输入：ICAISG_2026_SEAC_双盲投稿最终版.docx（匿名版，作者块为空）
输出：ICAISG_2026_SEAC_投稿修订版.docx
"""

import copy
from pathlib import Path

import docx

ROOT = Path(__file__).resolve().parent.parent
SRC = ROOT / "ICAISG_2026_SEAC_双盲投稿最终版.docx"
DST = ROOT / "ICAISG_2026_SEAC_投稿修订版.docx"

# ------------------------------------------------------------------ 新增文本
RELATED_WORK_PARA = (
    "Recent security research on RAG operates downstream of this interface. "
    "Traceback systems identify which stored texts are responsible for a "
    "misgeneration [18], responsibility-attribution frameworks score candidate "
    "passages after a poisoning incident [19], and context-attribution methods "
    "localize the parts of a long context that drove an answer [20]. All of them "
    "presuppose that the retrieval layer can name the units it returned; when "
    "chunk boundaries discard source identity, post-hoc attribution resolves only "
    "to an opaque passage. Chunking-side work has instead pursued representation "
    "quality, for example late chunking, which embeds a document before splitting "
    "it so that chunk vectors retain in-document context [21]. SEAC is "
    "complementary to both lines: rather than detecting or attributing corrupted "
    "evidence, it makes the returned evidence nameable in the first place, so that "
    "an auditor or a downstream traceback method resolves stable identifiers "
    "instead of reconstructing a citation from generated text."
)

NEW_REFERENCES = [
    'B. Zhang, H. Xin, M. Fang, Z. Liu, B. Yi, T. Li, and Z. Liu, "Traceback of '
    'poisoning attacks to retrieval-augmented generation," in Proc. ACM Web Conf. '
    "(WWW), 2025.",
    'B. Zhang, H. Xin, Y. Chen, Z. Liu, B. Yi, T. Li, L. Nie, Z. Liu, and M. Fang, '
    '"Who taught the lie? Responsibility attribution for poisoned knowledge in '
    'retrieval-augmented generation," in Proc. IEEE Symp. Security and Privacy '
    "(S&P), 2026.",
    'Y. Wang, W. Zou, R. Geng, and J. Jia, "TracLLM: A generic framework for '
    'attributing long context LLMs," in Proc. USENIX Security Symp., 2025.',
    'M. Gunther, I. Mohr, D. J. Williams, B. Wang, and H. Xiao, "Late chunking: '
    'Contextual chunk embeddings using long-context embedding models," '
    "arXiv:2409.04701, 2024.",
]


# ------------------------------------------------------------------ 工具函数
def clone_paragraph_after(anchor, text):
    """复制 anchor 段落的样式，插到其后并写入 text，返回新段落。"""
    new_p = copy.deepcopy(anchor._p)
    # 清空复制来的所有 run，只保留段落属性
    for child in list(new_p):
        if child.tag.endswith("}r") or child.tag.endswith("}hyperlink"):
            new_p.remove(child)
    anchor._p.addnext(new_p)
    para = docx.text.paragraph.Paragraph(new_p, anchor._parent)
    run = para.add_run(text)
    # run 字体沿用 anchor 首个 run，避免中文默认字体污染英文正文
    src_runs = anchor.runs
    if src_runs:
        run.font.name = src_runs[0].font.name or "Times New Roman"
        run.font.size = src_runs[0].font.size
    return para


def main():
    doc = docx.Document(str(SRC))
    ps = doc.paragraphs

    # --- 1. Related Work 末尾新增一段（P17 是该节最后一段正文） ---
    rw_last = ps[17]
    assert rw_last.text.startswith("Document conversion systems"), "Related Work 定位失败"
    clone_paragraph_after(rw_last, RELATED_WORK_PARA)

    # --- 2. 追加参考文献 ---
    refs = [p for p in doc.paragraphs if p.style.name == "references"]
    anchor = refs[-1]
    for entry in NEW_REFERENCES:
        anchor = clone_paragraph_after(anchor, entry)

    doc.save(str(DST))
    print("saved", DST)

    # 复核：段落数应为 101（原 100 段 + 1 段 Related Work），图片数应保持为 1
    out = docx.Document(str(DST))
    print("paragraph count:", len(out.paragraphs), "(source was 100)")
    print("inline images:", len(out.inline_shapes), "(source had 1)")
    print("references:", sum(1 for p in out.paragraphs if p.style.name == "references"))


if __name__ == "__main__":
    main()
