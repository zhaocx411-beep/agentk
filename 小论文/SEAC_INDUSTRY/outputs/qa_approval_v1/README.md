# 检索 QA 人工终审包 v1

状态：**待人工填写**。这是 P02–P10 候选题进入正式 QA 基准的唯一入口。

## 你需要做的

逐行填写 `approval_status`、`privacy_review_status`、`difficulty`、`review_notes`
四列（工作簿里是黄色底色），其余列不要改动——导入脚本会逐格比对，改动即中止。

**建议用终审工具填，而不是直接开 Excel：**

```bash
python3 scripts/serve_qa_approval.py
```

工具只监听 `127.0.0.1`，一次显示一条题，把 `gold_evidence_span` 在语料中**每一处**的
原文都取出来、跨度高亮、表格行按首行与证据行两行对齐渲染并标出承载跨度的列。这正是
工作簿单元格做不到的事：`gold_evidence_unit_ids` 在 xlsx 里是被截断的一串 id，没法
据此判断各处答案是否一致。`a`／`r` 定内容，`1`–`4` 定隐私，`e`／`m`／`h` 定难度，
`s` 采纳预审建议难度，`⏎` 保存并跳下一条未判，`?` 显示判定口径。校验规则与导入脚本
同源，能保存的行导入时不会被退回；每次保存即原子写回同一个 xlsx，可随时中断续填。
也可以照旧用 Excel 填，写的是同一个文件。

共 225 行：

| 项目 | 候选题数 | 其中多证据 | 需核对列语义 |
|---|---:|---:|---:|
| P02 | 32 | 6 | 0 |
| P03 | 33 | 8 | 0 |
| P04 | 28 | 9 | 0 |
| P05 | 25 | 5 | 0 |
| P06 | 22 | 2 | 0 |
| P07 | 23 | 3 | 0 |
| P08 | 24 | 4 | 1 |
| P09 | 20 | 2 | 2 |
| P10 | 18 | 4 | 0 |
| 合计 | 225 | 43 | 3 |

隐私列必须由获授权判定企业文档的人填写。内容审核与隐私放行可以是不同的人，但
两列都填完该行才算判定完成。

## 四列口径

`approval_status` 只能是 `APPROVED` 或 `REJECTED`。判 `APPROVED` 的前提是：题干
自然且自足（不依赖文件名、块编号或未说明的上下文）、答案能由 `gold_evidence_span`
直接支持、不存在同样合理的冲突答案。凑题数不是放行理由，宁可 `REJECTED`。

`privacy_review_status` 只能是 `NO_RISK`、`ANONYMIZED`、`CLEARED`、`REJECTED`。
类别定义与处理顺序见 `../../docs/annotation_and_privacy_protocol.md` 第 7 节。
判 `ANONYMIZED` 必须在 `review_notes` 写明替换规则；判 `REJECTED` 必须写明理由。
任何真实口令、密钥、连接串一律 `REJECTED`，不能靠掩码保留可复原结构。

`difficulty` 只能是 `Easy`／`Medium`／`Hard`，且只有 `APPROVED` 行需要填。
`suggested_difficulty` 是预审建议，不具约束力。

`review_notes` 在三种情况下必填：任一列判 `REJECTED`、隐私判 `ANONYMIZED`、
以及 `needs_role_check` 为 `YES` 的行。

## 三个需要特别注意的列

`evidence_occurrences` 大于 1 表示同一段原文在语料多处出现。这是企业文档逐字复用
的正常结果，检索命中其中任一处都算正确，所以重复本身不是拒绝理由。但请确认这些
位置的答案确实一致——如果不同位置给出不同事实，那是两道题混成了一道，应 `REJECTED`。
工具会列出每一处的文档名与标题路径，据此判断它们是否真的等价。

`needs_role_check` 为 `YES` 表示证据所在表格的表头有重复列名或占位列名（例如整行
都是「主档」）。这种情况下文字匹配无法保证答案没有给某一列指派表头并未声明的语义
角色，需要你核对列序，并在 `review_notes` 写明核对结论。共 3 行：`P08-QS003`、
`P09-QS001`、`P09-QS005`。

`answer_grounding` 有 81 行是空的，即预审直接通过（69 行）与按多证据放行（12 行）
这两批——它们没有经过改写校验，所以没有机械得出的答案支撑结论。工具会另行标出答案
是否为证据的字面子串；标为「需人工判断」的行，要自己确认改述没有引入证据以外的
信息。剩余 144 行的 `LITERAL`／`PARAPHRASE_GROUNDED` 是改写校验的结论，仍需你复核。

## 填完之后

```bash
python3 scripts/import_qa_approval_workbook.py          # 只报告，不写文件
python3 scripts/import_qa_approval_workbook.py --write  # 全部行判定完成后才写
python3 scripts/audit_formal_readiness.py
```

导入脚本会拒绝：候选列被改动、任一行未判定或只判了一半、`APPROVED` 缺难度、
该写理由却没写、以及证据片段在当前语料中已找不到或出现次数超过上限。只有内容
`APPROVED` 且隐私为 `NO_RISK`／`ANONYMIZED`／`CLEARED` 的记录会写入
`workbench/<project>/qa_approved.json`，判定明细留在 `import_report.json`。

另有一条机械规则会**覆盖**放行判定：题干以小节编号开头的行按规程 §6.1bis 排除，
不写入正式文件。它不改工作簿，命中行连同其原判定记入 `import_report.json` 的
`rule_excluded` 并在摘要中打印。判据只认「1–2 位数字 + 至少一个点分段」，因此
`2.1.2.1获取设备列表接口……` 命中，而 `2025.7.22召开的……` 这类正文日期不命中。

## 本轮结果

225 行已全部判定：放行 205、拒绝 20，其中 16 条拒绝是同一个缺陷（题干把小节名当作
提问对象）。规则又排除了 10 条放行题（P03 四条、P04 两条、P05 四条），最终写出 195 条。
P10 保留 11 条低于 15 条下限，按 §8bis 声明退出检索指标。

## 保密

含企业文档片段，仅限受控环境内部使用，不得作为论文附件或上传公开平台。
