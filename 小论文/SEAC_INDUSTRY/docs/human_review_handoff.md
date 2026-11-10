# 人工待办交接说明

适用范围：SEAC 工业文档实验的全部人工判定环节  
当前门禁状态：**`10/10` 项目 `FORMAL_READY`，`formal_ready: true`**
（`python3 scripts/audit_formal_readiness.py --require-ready`）

**三轮人工判定全部完成，已无阻塞项。** 边界验证、检索 QA 终审、P01 隐私放行都已判完
并通过各自的导入门禁。本文保留为留痕与复现说明：记录谁填了什么、每列的口径、以及各轮
之后跑了哪几条命令。剩下的事项是硬件与授权类（见文末），不再涉及标注判定。

## 三件待办

| # | 待办 | 产物 | 行数 | 需要什么资质 | 状态 |
|---|---|---|---:|---|---|
| 1 | 边界预标验证 | `outputs/prelabel_verification_v1/SEAC_boundary_prelabel_verification.xlsx` | 1,050 | 熟悉标注规程的标注员 | **已完成并放行**（P08 除外） |
| 2 | 检索 QA 终审 | `outputs/qa_approval_v1/SEAC_QA_approval.xlsx` | 225 | 内容审核员 + 获授权的隐私审核人 | **已完成并放行**（P10 退出检索） |
| 3 | P01 隐私放行 | `outputs/annotation_workbooks_v1/SEAC_QA_privacy_review.xlsx` 的 P01 表 | 27 | 获授权的隐私审核人 | **已完成并放行**（拒 3 题，保留 24 题） |

三件事互相独立。每个包内的 `README.md` 是给填表人的详细口径说明，本文只讲全局顺序和
它们各自解锁了什么。三轮均已完成，`audit_formal_readiness.py --require-ready` 现在
返回 `formal_ready: true`。

## 待办 1：边界预标验证（已完成，2,400 条边界已放行）

1,050 行已全部判定，8 个项目通过放行门禁，共 2,400 条边界金标准已写出，来源构成为
人工验证 783 条（32.6%）、§4.1 规则判定 387 条（16.1%）、模型预标 1,230 条（51.2%）。
P08 人机 kappa 0.650 未达 0.70，其边界金标准不予放行，边界指标不报告 P08；判定与
披露口径见规程 §4.1bis 与 §4.1bis.7。若需重跑：

```bash
python3 scripts/promote_llm_prelabels.py                       # 只读，输出两组 kappa
python3 scripts/promote_llm_prelabels.py --withhold P08 --write
```

`--withhold` 只接受确实未达标的项目，脚本会拒绝用它排除已通过的项目。以下为原始
填写说明，保留供后续轮次参考。

## 待办 1 的填写说明（供后续轮次参考）

P02–P10 的 2,700 个边界候选已由 LLM 全量预标。人工要做的不是重标全量，而是盲验
一个分层随机样本加上全部预标阶段判不准的难例，用人机一致率决定预标能否放行。

**用标注工具填，不要直接开 Excel。** 表格行单元在单元格里会挤成一长串带重复表头的
文字，一半以上的行都是这种，在 Excel 里辨认成本极高：

```bash
python3 scripts/serve_boundary_verification.py
```

只监听 `127.0.0.1`，一次显示一条边界，表头与数据行渲染成对齐单元格、重复的合并
单元格列自动合并；`1`／`2`／`3` 打标后自动跳下一条未判，`d` 标歧义，`u` 跳到下一条
未判，`?` 显示判定口径。每次打标即原子写回同一个 xlsx，只动那三列，可随时中断续标。
工具不读取任何预标文件，盲态与手填等价；也可以照旧用 Excel 填，写的是同一个文件。

详细口径见 `outputs/prelabel_verification_v1/README.md`。三点最容易出错的：

1. **不要先看预标签**。工作簿刻意不含预标结果，填写前不要查阅
   `outputs/llm_prelabel_v1/labels/`，否则一致率失去意义。
2. 只填 `human_label`、`review_status`、`review_notes` 三列，标签只能是
   `BREAK`／`NO_BREAK`／`NOISE`。
3. `row_group` 为 `AGREEMENT_SAMPLE` 的行参与 kappa 统计，`FLAGGED_REVIEW` 的难例
   必须判定但单独报告、不计入一致率。
4. 拿不准的行填最佳判断并把 `review_status` 设为 `NEEDS_DISCUSSION`，同时在
   `review_notes` 写明歧义来源（无理由的歧义标记会被门禁拒绝）。这些行照常放行、
   照常计入一致率——把标注员自己标难的行从统计里剔掉会系统性地虚高一致率。

填完之后：

```bash
python3 scripts/promote_llm_prelabels.py            # 只读检查，输出各项目一致率与 kappa
python3 scripts/promote_llm_prelabels.py --write    # 全部达标后写出正式金标准
```

门禁会拒绝未修改的模板、缺行多行、非法标签、以及任一项目 `kappa < 0.70`。人工标签
与预标冲突时一律以人工为准。写出的每条边界都带 `label_source` 与 `human_verified`，
论文必须报告人工验证与模型预标各自的占比。

## 待办 2：检索 QA 终审（已完成，195 条正式问题已写出）

225 条候选题已全部判定：放行 205 条、拒绝 20 条。终审后又按规程 §6.1bis 的机械判据
排除了 10 条题干以小节编号开头的放行题（P03 四条、P04 两条、P05 四条），最终写出
195 条，分布为 P02 31、P03 28、P04 22、P05 17、P06 22、P07 23、P08 22、P09 19、P10 11。

20 条拒绝里有 16 条是同一个生成器缺陷：题干把文档小节名当作提问对象（「〈小节名〉中
描述了什么数量、时长或比例要求」），提问范围由小节而非题干界定，违反 §6.1。其余 4 条
是一条文件名日期依赖与三条近重复。被拒行的备注里多数带有具体改写建议，可按 §6ter.2
在后续轮次重新入池。

P10 只保留 11 条，低于 15 条下限，按 §8bis 声明 `qa_withheld` 退出检索指标；它的 11 条
问题本身有效，仍保留在 `qa_approved.json`，边界指标不受影响。若需重跑导入：

```bash
python3 scripts/import_qa_approval_workbook.py          # 只报告，含规则排除清单
python3 scripts/import_qa_approval_workbook.py --write
```

以下为原始填写说明，保留供后续轮次参考。

## 待办 2 的填写说明（供后续轮次参考）

225 条候选题需要内容终审与隐私放行。候选题的来源分四类，`candidate_state` 列会标明：
预审直接通过 69 条、经改写并通过机械校验 114 条、按多证据模型放行 12 条、以及为补足
题数补写的草稿 30 条。不同来源的判定标准相同——都要求题干自足、答案由证据直接支持。

**用终审工具填，不要直接开 Excel。** 多证据模型下，一条题是否成立取决于它的证据在
语料里的每一处是否都支持同一个答案，而工作簿单元格只能放一串被截断的 unit id：

```bash
python3 scripts/serve_qa_approval.py
```

只监听 `127.0.0.1`，一次显示一条题：题干、参考答案、证据跨度，加上该跨度在语料中
**每一处**的原文，跨度在原文里高亮，表格行按首行与证据行两行对齐渲染并标出承载跨度
的列。`a`／`r` 定内容，`1`–`4` 定隐私，`e`／`m`／`h` 定难度，`s` 采纳预审建议难度，
`⏎` 保存并跳下一条未判，`?` 显示判定口径。校验规则直接复用导入脚本，能保存的行导入
时不会被退回。每次保存即原子写回同一个 xlsx，只动那四列，可随时中断续填。

详细口径见 `outputs/qa_approval_v1/README.md`。四点这批数据特有的：

1. `evidence_occurrences` 大于 1 是正常的（43 条如此），检索命中任一等价位置都算
   正确，重复本身不构成拒绝理由；但要确认各位置的答案一致。工具会把各位置的文档名
   与标题路径列出来，据此判断它们是否真的等价。
2. `needs_role_check` 为 `YES` 的 3 行（`P08-QS003`、`P09-QS001`、`P09-QS005`）需要
   核对表格列序，确认答案没有给某一列指派表头并未声明的语义角色。这 3 行放行时必须
   在备注里写下核对结论，否则导入会退回。
3. `answer_grounding` 有 81 行是空的——预审直接通过与多证据放行这两批没有经过改写
   校验，因此没有机械的答案支撑结论。工具会另行标出答案是否为证据的字面子串；标为
   「需人工判断」的行要自己确认改述没有引入证据以外的信息。
4. 拒绝、判 `ANONYMIZED`、以及放行表头存疑的行都必须写备注；被拒的行不要填难度。
5. 题干不得以小节编号开头（如 `2.1.2.1获取设备列表接口……`）。这一条由导入器按
   §6.1bis 机械判定，命中行即使判为 `APPROVED` 也不会写入正式文件，因此终审时直接
   拒掉更省事。判据只认「1–2 位数字 + 至少一个点分段」，`2025.7.22召开的……` 这类
   正文日期不受影响。

填完之后：

```bash
python3 scripts/import_qa_approval_workbook.py          # 只报告
python3 scripts/import_qa_approval_workbook.py --write  # 全部行判定完成后才写
```

## 待办 3：P01 隐私放行（已完成，24 题已放行）

P01 的 27 条 QA 内容审核早已完成（`audit_status=KEEP`），此前卡在隐私放行：预筛把全部
27 条标为 `CUSTOMER_OR_VENDOR_IDENTITY`，隐私状态是 `PENDING`。该轮已判完，结果：

| 隐私结论 | 题数 | 说明 |
|---|---:|---|
| `NO_RISK` | 23 | 客户标识只出现在文档名或章节路径，不随基准发布 |
| `CLEARED` | 1 | 存在标记但经授权可用于当前受控实验 |
| `REJECTED` | 3 | QA020、QA029、QA035，客户或关联企业名出现在答案与证据跨度里 |

三条拒绝按 §7bis 连带把 `approval_status` 一并置为 `REJECTED`，最终保留 24 题，
高于下限 15。三条都未采用就地改写脱敏，因为改写证据跨度会破坏第 8 节的跨度包含校验。

其中 QA020 的关联企业名不在自动推导的标识内，是审核人通读原文识别出来的——这印证了
§7bis 末段的告知：高亮只是辅助，不构成查全保证。

### 推荐用本地复核工具

```bash
python3 scripts/serve_privacy_review.py       # 默认 P01，端口 8792
```

直接在 Excel 里判这 27 行有个陷阱：预筛是把题干、答案、证据跨度、文档名、章节路径
**合在一起**扫描后打标的，所以标记本身不说明客户名出现在哪个字段——而这恰恰是要判的
事。本工具按行定位每一处出现并高亮，在顶部直接给出结论所需的区分：

- **仅元数据命中**（本批 25 行）：题干、答案、证据跨度都不含客户名，命中来自文档名或
  章节路径，这两个字段只留在工作台文件里，不随基准发布。
- **内容命中**（本批 2 行，QA029 与 QA035）：客户名出现在参考答案与证据跨度里，放行
  等于把它带进基准。

客户标识由语料文件名的最长公共前缀自动推出，不必写进命令行；页面会列出实际匹配的
标识，若客户名的另一种写法不在其中，用 `--identifier` 补充后重开。

工具同时挡住两个会在几条命令之后才暴露的耦合：

- 判 `REJECTED` 时会自动把同行 `approval_status` 也改为 `REJECTED`，因为导入器不接受
  「隐私拒绝而内容仍批准」。P01 共 27 题、下限 15 题，最多可拒 12 题。
- 不要为脱敏就地改写 `gold_evidence_span`：下游 `finalize_qa_review.py` 要校验跨度
  逐字（忽略空白）出现在语料单元里，改了就会被判「片段不在单元内」。要脱敏就改参考
  答案并判 `ANONYMIZED`，在备注写明替换规则。

校验规则与 `import_review_workbooks.py` 同源，能保存的行导入时不会被退回；保存即原子
写回同一个工作簿，可随时中断续填。

填完后实际执行的命令（`--boundary-projects` 后不跟参数即跳过边界导入，因为 P02–P10
边界已改走预标验证路径，不再需要 A/B 工作簿）。已跑完，重跑会被拒绝覆盖已有 review，
这是有意的防护：

```bash
python3 scripts/import_review_workbooks.py \
  --qa outputs/annotation_workbooks_v1/SEAC_QA_privacy_review.xlsx \
  --qa-projects P01 --boundary-projects --check-only

python3 scripts/import_review_workbooks.py \
  --qa outputs/annotation_workbooks_v1/SEAC_QA_privacy_review.xlsx \
  --qa-projects P01 --boundary-projects

python3 scripts/finalize_qa_review.py \
  --corpus workbench/P01/corpus.json \
  --qa-review workbench/P01/annotation_review/qa_reviewed.json \
  --approved-output workbench/P01/qa_approved.json \
  --summary-output workbench/P01/qa_approval_summary.json \
  --require-ready
```

注意导入脚本会拒绝「内容 `APPROVED` 但隐私仍 `PENDING`」的行，所以 27 行必须一次
填完。`qa_privacy_review_pending.json` 是不可覆盖的输入记录，正式文件是
`qa_approved.json`。

## 三件都做完之后（当前所处位置）

```bash
python3 scripts/audit_formal_readiness.py --require-ready
python3 scripts/run_formal_suite.py --device cuda        # bge-m3，需 GPU
python3 scripts/analyze_formal_results.py
python3 scripts/fill_manuscript_results.py
python3 scripts/lint_manuscript.py
python3 scripts/build_submission_docx.py
```

`audit_formal_readiness.py --require-ready` 必须先通过；它检查每个项目的边界数、
边界标签完整性、QA 题数下限、批准状态、隐私放行状态和精确证据。正式套件跑完前
不要查阅 P04–P10 的任何检索指标，锁定测试集只允许在配置冻结后访问一次。

按 §8bis 声明退出的轴由套件传入 `none`，该轴不评测、不写明细文件、CSV 对应列留空。
当前有两处：P08 退出边界指标，P10 退出检索指标。

论文的 143 个结果占位符由 `fill_manuscript_results.py` 一次性注入，其中包含标注
来源的口径（人工验证占比、模型预标占比、人机 kappa、QA 候选集规模、终审放行与规则
排除的题数），所以论文里报告的题数是**人工最终保留并通过规则**的 195 条，不是这里的
225 条候选数，也不是终审放行的 205 条。

## 不要做的事

不要为了让门禁通过而放宽任何判定：门禁拒绝时正确的做法是拒掉那道题或那条边界，
而不是改判定。不要用脚本填写 `approval_status`、`privacy_review_status` 或
`human_label`——这三列的人工来源是论文可信度的基础，也是 AI 声明里已经写明的边界。
三个工作簿都含企业文档片段，仅限受控环境内部使用。
