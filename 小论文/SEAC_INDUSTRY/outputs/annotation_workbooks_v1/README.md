# SEAC 人工审核工作簿 v1

状态：**全部人工判定已完成**。P01 表 27 行已判完(23 `NO_RISK`、1 `CLEARED`、
3 `REJECTED`),已导入并 finalize,保留 24 题。

- `SEAC_QA_privacy_review.xlsx`：P01–P10，共 289 条候选 QA；由获准接触企业材料的
  QA/隐私审核员处理。P01 表的 27 行已完成；其余项目改走
  `outputs/qa_approval_v1/` 的候选集终审路径，亦已完成。
- `SEAC_boundary_annotator_A.xlsx` / `SEAC_boundary_annotator_B.xlsx`：**已废弃**。
  P02–P10 的边界标注改走规程第 4bis 节的「预标 + 人工验证」路径，产物在
  `outputs/prelabel_verification_v1/`，该轮已完成。这两个文件只作为历史记录保留，
  不要再填写。

QA 审核员必须分别完成内容审批和隐私审批；自动候选不是金标准。完整规则见
`../../docs/annotation_and_privacy_protocol.md`，隐私一轮的口径与录入方式见其第 7 节
与第 7bis 节。

工作簿包含企业文档片段、项目名称和潜在敏感信息，只能在受控环境内部使用，不能
上传到公开标注平台、作为论文附件或对外分发。

## 填 P01 那 27 行（已完成，以下为留痕）

推荐用本机复核界面，它按字段定位并高亮每一处客户标识出现，并挡住两处跨脚本耦合
（隐私拒绝须连带拒绝内容；不得为脱敏就地改写证据跨度）。注意高亮只覆盖自动推导的
标识，正文里的第三方主体需通读识别——本批 QA020 即属此例：

```bash
python3 scripts/serve_privacy_review.py
```

也可以直接在 Excel 里填 `privacy_review_status` 与 `privacy_review_notes`。要注意
预筛是把题干、答案、证据跨度、文档名、章节路径合并后匹配的，所以
`privacy_flags` 不指明命中字段——本批 25 行的客户名只出现在文档名里，只有 QA029 与
QA035 出现在参考答案与证据跨度里。

填完后先执行只读导入检查（不带边界工作簿；`--boundary-projects` 后不跟参数即跳过
边界导入）：

```bash
python3 scripts/import_review_workbooks.py \
  --qa outputs/annotation_workbooks_v1/SEAC_QA_privacy_review.xlsx \
  --qa-projects P01 --boundary-projects \
  --workbench-root workbench \
  --check-only
```

只有检查无误后才去掉 `--check-only`。初始文件哈希、项目数量和门禁状态记录在
`workbook_manifest.json`。
