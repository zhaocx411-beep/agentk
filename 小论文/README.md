# ICAISG 2026 投稿：SEAC 论文

投稿目标：2026 年第二届人工智能安全与治理国际会议（ICAISG 2026），杭州，2026 年 11 月 20–22 日。
IEEE 出版，EI Compendex / Scopus 检索。截稿 2026-09-15。

## 稿件文件

| 文件 | 说明 |
|---|---|
| `ICAISG_2026_SEAC_双盲投稿最终版.docx` | 匿名版原稿，作者块为空。**不要直接改这个文件** |
| `ICAISG_2026_SEAC_投稿修订版.docx` | 中间产物：原稿 + Related Work 一段 + 4 条参考文献 |
| `ICAISG_2026_SEAC_投稿版.docx` | **最终投稿文件**，在修订版基础上填入作者块 |

## 评审方式

会议秘书 2026-09-01 回复：文章采用双盲审稿，但**投稿时须提交含作者信息的版本**，
会务记录文章信息后，送审前自行删除作者信息。因此投稿文件保留完整作者块，
正文无需为双盲做额外处理。

## 脚本

依赖装在 `.venv_fig/`（python-docx）。两个脚本按顺序执行：

```bash
./.venv_fig/bin/python figures/apply_paper_edits.py   # 原稿 -> 投稿修订版
./.venv_fig/bin/python figures/fill_authors.py        # 投稿修订版 -> 投稿版
```

`apply_paper_edits.py` 在原稿上做增量插入，只增不删，共两处改动：

1. Related Work 末尾新增一段，把 SEAC 定位到 RAG 取证与责任归因这条安全线索上，
   引用新增的 [18]–[21]。这一段不可省略，IEEE 不接受未被正文引用的参考文献条目。
2. 参考文献表末尾追加 4 条 2024–2026 年文献（WWW 2025、IEEE S&P 2026、
   USENIX Security 2025、late chunking）。

`fill_authors.py` 填写作者块：把作者块所在节的栏数由模板预设的 4 改为实际的 3，
三位作者之间用显式分栏符隔开（不依赖 Word 自动栏平衡，各作者行数不等时会错位），
并同步写入文档属性的标题与作者字段。

## 投稿前待办

1. 在 Word / WPS 中打开投稿版，确认三位作者并排成三栏、`*Corresponding author`
   紧跟在第一作者邮箱下方。
2. 核对邮箱域名是否应为 `@bupt.edu.cn`。目前填的是给定的 `2024110375@bupt.cn`。
3. 补齐 Xiai Wang 与 Yongjun Zhang 的邮箱（目前只有通讯作者一个邮箱，
   IEEE 允许，但三位齐全更规范）。
4. 导出 PDF 并确认总页数在 5–6 页之间（超过 6 页每页 60 USD / 400 RMB）。
5. 投稿：EasyChair `icaisg2026` 或邮箱 `icaisg_contact@yeah.net`，
   邮件主题格式 `Submission-ICAISG 2026-Full Paper`。两种方式二选一，勿重复投稿。

## 待议的修改建议（尚未实施）

- 把"因为是冻结评估，去掉 parent fusion 后重报结果会使锁定评估退化为开发证据"这句话
  从讨论部分前移到表 III 附近。这是审稿人最可能提出的质疑点，目前解释出现得偏晚。
