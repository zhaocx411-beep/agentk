# SEAC 人工标注、仲裁与隐私审查规程

规程版本：`industrial-seac-annotation-review-v1.4`  
适用范围：P02–P10 边界标注与检索 QA 审查  
冻结种子：`20260804`

v1.1 相对 v1.0 增补第 4bis 节（LLM 预标 + 人工抽样验证路径）、第 6bis 节
（QA 内容预审）与第 6ter 节（多证据模型与候选集构建），并把第 6 节第 5 条的证据
唯一性要求改为按第 6ter 节的等价证据集判定。

v1.2 相对 v1.1 增补第 4.1bis 节（三类确定性噪声的判据、其事后规格化的披露、以及
命中行不计入一致率的规定）与第 6ter.5 节（终审录入方式）。第 4.1bis 节是在第一轮
边界验证结果产出之后补定的，该节第 5 条如实记录了这一时点。

v1.3 相对 v1.2 增补第 6.1bis 节（题干自足性的机械判据）与第 8bis 节（单轴退出的
声明方式）。第 6.1bis 节同样是在第一轮 QA 终审完成之后补定的，该节末尾如实记录了
这一时点。

v1.4 相对 v1.3 增补第 7bis 节（隐私审查录入方式）。该节澄清自动预筛是合并字段后
匹配的，因此标记不指明命中字段，并要求审核人按字段确认后再给结论；隐私类别与四种
结论取值均未改动。

四次修订均未改动标签定义、隐私类别、QA 门禁与放行清单，`approval_status` 与
`privacy_review_status` 仍只能由人工填写；第 6.1bis 节的机械判据不写回工作簿。
P01 仍按 v1.0 的双人独立标注与仲裁执行。

## 1. 目的与不可变原则

本规程用于把盲标包转化为可用于论文正式实验的人工金标准。标注员不得查看
SEAC 或任何基线的预测、分数、检索排名和实验结果。测试项目 P04–P10 在标注、
隐私审查和配置锁定完成前，不得运行或查看正式检索指标。

自动生成的边界候选和 QA 题目都只是待审材料，不是人工金标准。任何空标签、
模型建议、规则生成标签或未经批准的 QA 都不得进入论文表格。

## 2. 角色与职责分离

- 标注员 A、B：独立完成同一项目的 300 个边界，不得互相讨论具体样本。
- QA 审核员：核对问题、答案、证据单元和证据片段；可以与边界标注员是同一人，
  但不得参考方法输出。
- 隐私审核员：核对公开风险并记录处理方式；高风险记录不得由题目生成者单独放行。
- 仲裁员：只处理 A、B 的分歧或 `NEEDS_DISCUSSION` 样本，并给出可复核的理由。
- 实验执行人：仅接收通过脚本门禁生成的正式金标准，不修改人工标签。

如人员有限，至少保证边界 A/B 相互独立，并由第三次判断完成分歧仲裁。论文中按
实际情况披露标注员人数、背景、训练过程和是否获得报酬，不虚构人员或过程。

## 3. 标注前校准

两名边界标注员先使用 P01 已仲裁样本各自完成不少于 30 个练习，覆盖标题切换、
同节连续正文、表内行、表格进出和噪声。练习样本不计入 P02–P10 的 300 个正式
样本。完成后仅讨论规则，不讨论正式样本。

冻结的 30 题校准材料位于：

- `outputs/calibration_workbooks_v1/SEAC_boundary_calibration_exercise.xlsx`：分别发给
  A、B 独立填写；
- `outputs/calibration_workbooks_v1/SEAC_boundary_calibration_key.xlsx`：由标注负责人
  保管，在两人均交回练习后才发放；
- `outputs/calibration_workbooks_v1/calibration_manifest.json`：记录种子、分层分布、
  难例数、样本 ID 和文件哈希。

练习集仅来自开发项目 P01，包含 9 种“边界类型 × 最终标签”组合和 6 个曾经需要
裁决的难例，不含 P02–P10 正式样本。两人对照答案后，应记录已讨论的规则要点，
不得把答案册带入正式标注过程。

标注判断只回答一个问题：在不考虑当前算法预测、但长度允许的情况下，右侧单元
是否应开始一个新的、语义上可独立检索的父块？

## 4. 边界标签判定

允许的标签只有 `BREAK`、`NO_BREAK` 和 `NOISE`。

### 4.1 优先判为 `NOISE`

只要左右任一单元主要是页眉页脚、孤立页码、目录导航、空模板、无意义占位符、
转换乱码或无法形成内容证据的残片，就标记 `NOISE`。不要把“内容很短”直接等同
于噪声；接口方法名、字段名、约束值等短文本仍可能是有效证据。

#### 4.1bis 三类确定性噪声（v1.2 增补，补定时点见 4.1bis.5）

第一轮人工验证暴露出三类相邻单元：它们确实不构成可检索证据，但未被上面的清单
逐项列出，于是人工验证者与 LLM 预标各自只对其中一部分套用了“噪声优先”条款。
每一类的分歧方向都是 100% 单向的——这是定义空缺的特征，而不是标注疏忽的特征。

本节把上文已有的“任一单元……无法形成内容证据的残片”条款对这三类写成显式判据。
判据只看证据文本，不看任何人给出的标签；只在单元**整体**即为骨架时命中，任何携带
实质内容的单元仍交人工判断。

**4.1bis.1 裸结构标签**：单元整体只是一个表单槽位名（“输出”“输入”“处理逻辑”
“返回信息”“请求信息样例”“无”等），它命名槽位而不填充槽位，任何提问都无法由它作答。

**4.1bis.2 转换残片**：单元整体只由括号、引号、分隔符与数字构成（如 `}`、`],`），
属格式转换遗留物。

**4.1bis.3 落款与签署块**：单元整体为机构名加冒号的落款、签署角色（“记录人：”
“盖章”）或裸日期行，属文档家具。

**4.1bis.4 与语料统计的独立佐证**：前两类不依赖分歧观察即可确认。在 P02 单篇详细
设计规格书内，“输出”出现 289 次、“处理逻辑”288 次、“输入”201 次；在 P06 单篇文档内
`}` 出现 267 次、`{` 258 次、“处理逻辑”210 次。一个词条在单篇文档内重复两三百次，
客观上即为表单骨架而非内容证据。第 4.1bis.3 类的佐证较弱，依据是纪要汇总文档内
落款成对出现 8 次的规律。

**4.1bis.5 事后规格化的如实披露**：这三类判据是在第一轮验证结果产出**之后**、
依据观察到的分歧总结出来的，属事后规格化（post-hoc specification）。论文必须同时
报告补定前与补定后两组 kappa，不得只报有利的一组。放行摘要逐项目保存
`cohen_kappa_before_rule_amendment` 与 `cohen_kappa` 两个字段即为此用。补定后
P02 由 0.605 升至 0.869、P06 由 0.417 升至 0.910，但 P08 由 0.675 降至 0.650、
P09 由 0.745 降至 0.721、P10 由 0.770 降至 0.763——判据在这三类罕见的项目上降低
了一致率，可作为“判据并非单纯提升 kappa 的装置”的旁证，此点亦须如实报告。

**4.1bis.6 命中行不计入一致率**：由本节判据决定的行，其标签不来自任何一方的判断，
因此不计入人机一致率统计，只单独报告命中数与命中原因。判据不修改工作簿里标注员
填写的记录；归一化只发生在放行阶段，并逐行写入 `label_source=RULE_4_1` 与
`rule_reason`，保留可审计的来源。

**4.1bis.7 不得为通过门禁而扩类**：新增噪声类别必须有独立于标签的依据。P08 残余
分歧为封面/扉页元素（机构名与项目名，单篇内仅出现 1–2 次，不构成页眉页脚），无频次
依据支持，因此**不**新增该类别；P08 边界金标准不予放行，边界指标不报告 P08。

### 4.2 判为 `BREAK`

满足下列任一情形，且右侧能开启新的语义单元时，通常标记 `BREAK`：

- 进入新的同级或更高层章节、功能、接口、数据表、会议议题或需求主题；
- 左侧主题已经闭合，右侧开始独立事实、流程或设计对象；
- 从一个表格退出并开始新的正文主题，或从正文进入一个新的独立表格；
- 相邻内容虽同属一个大章节，但单独检索时应分别返回，合并会明显混淆证据。

标题本身并不自动等于断点。若标题必须与紧随其后的首段共同解释，应在“上一主题
到新标题”处断开，而在“新标题到其首段”处保持连续。

### 4.3 判为 `NO_BREAK`

满足下列情形时通常标记 `NO_BREAK`：

- 右侧是左侧句子、列表、步骤、条件、示例或说明的延续；
- 标题与其首个内容单元之间存在直接从属关系；
- 同一接口、同一需求、同一流程或同一设计对象的相邻属性仍共同表达一个主题；
- 同一张表内的表头与数据行、或相邻数据行属于同一父级表格。行级检索由
  `gold_evidence_unit_id` 和 SEAC 子块处理，不通过父块边界标签强行拆表。

不得因为文本超过或接近 512 token 就标记 `BREAK`；长度约束由切块器处理，人工
标签评价的是语义边界。

### 4.4 歧义处理

标注员仍需在 `human_label` 填写其最佳判断，并将 `review_status` 设为
`NEEDS_DISCUSSION`，在 `review_notes` 中用一句话写明歧义来源。不得留空来代替
判断，也不得查看另一名标注员答案。

### 4.5 工作簿操作与回收

正式填写以下三个内部工作簿：

- `outputs/annotation_workbooks_v1/SEAC_boundary_annotator_A.xlsx`：只交给标注员 A；
- `outputs/annotation_workbooks_v1/SEAC_boundary_annotator_B.xlsx`：只交给标注员 B；
- `outputs/annotation_workbooks_v1/SEAC_QA_privacy_review.xlsx`：只交给获准查看企业材料的
  QA/隐私审核员。

边界工作簿只修改 `human_label`、`review_status` 和 `review_notes`。QA 工作簿允许根据
原文修正问题、答案、来源、证据和分类字段，但不得修改 `qa_id` 或 `project_id`。
在填写过程中，可随时运行只读进度审计；它区分 `complete`、`pending`、`partial` 和
`invalid`，不会写入或覆盖任何审核数据：

```bash
python3 scripts/audit_review_progress.py
```

三个工作簿全部完成后，再运行严格导入校验：

```bash
python3 scripts/import_review_workbooks.py \
  --boundary-a outputs/annotation_workbooks_v1/SEAC_boundary_annotator_A.xlsx \
  --boundary-b outputs/annotation_workbooks_v1/SEAC_boundary_annotator_B.xlsx \
  --qa outputs/annotation_workbooks_v1/SEAC_QA_privacy_review.xlsx \
  --workbench-root workbench \
  --check-only
```

校验通过后去掉 `--check-only`，脚本才会在各项目的 `annotation_review/` 目录写入
审核 JSON，并生成输入、输出 SHA-256 摘要。脚本会拒绝漏行、重复 ID、被修改的
边界证据、未完成标签、批准但未隐私放行的 QA，以及无理由的拒绝或匿名化。若目标
文件已存在，默认停止而不覆盖；确需替换时应先归档旧版本，再显式使用 `--force`。

三个工作簿包含原始企业证据，仅限受控环境内部流转，不得作为论文附件、公开数据或
云端通用标注服务输入。A/B 工作簿不得互换，文件哈希应随标注记录保存。

## 4bis. LLM 预标 + 人工抽样验证路径（v1.1 增补）

规程 v1.0 只定义了 A/B 双人独立标注一条路径。因实际人力限制，P02–P10 改走本节
定义的第二条路径。该路径必须如实披露，不得表述为双人人工标注。

### 4bis.1 执行顺序

1. `scripts/prepare_llm_prelabel_batches.py`：从 `boundary_blind_master.json` 抽取
   批次，只带标注员可见字段，不含任何方法预测。
2. LLM 按 `outputs/llm_prelabel_v1/labeling_rules_applied.md` 逐条判断，产出标签、
   决定性依据和 `NEEDS_HUMAN_REVIEW` 标记。
3. `scripts/merge_llm_prelabels.py`：校验覆盖率、顺序、取值与输入哈希后合并，
   每条记录写入 `label_source=LLM_PRELABEL`、`human_verified=false`。
4. `scripts/audit_prelabel_consistency.py`：机械审计确定性规则冲突与分层分布离群，
   未标复核的规则冲突必须为 0。
5. `scripts/build_prelabel_verification_sample.py`：生成盲态人工验证工作簿。
6. 人工独立填写 `human_label`、`review_status`、`review_notes`。可直接编辑工作簿，
   也可用 `scripts/serve_boundary_verification.py` 提供的本机标注界面；后者只是同一
   工作簿同一三列的录入方式，逐条原子写回，不读取任何预标文件，盲态要求不变。
7. `scripts/promote_llm_prelabels.py`：通过门禁后写出正式金标准。

### 4bis.2 抽样设计

每项目取两组互不重叠的行：

- `AGREEMENT_SAMPLE`：按分层比例、固定种子 `20260804` 抽取的 100 条随机样本，
  只用它统计人机原始一致率与 Cohen's kappa；
- `FLAGGED_REVIEW`：预标阶段标记 `NEEDS_HUMAN_REVIEW` 且未落入随机样本的全部行，
  必须人工判定，但单独报告，不计入一致率，避免难例拉低估计。

验证工作簿不含预标签、预标依据和任何方法输出，验证者填写前不得查阅预标文件。

### 4bis.3 放行条件

- 验证行全部带合法标签，且 `review_status` 为 `COMPLETE` 或 `NEEDS_DISCUSSION`；
  按 4.4 标为 `NEEDS_DISCUSSION` 的行必须在 `review_notes` 写明歧义来源，且照常
  计入一致率统计——剔除标注员自述的难例会系统性虚高一致率。放行摘要逐项目报告
  `needs_discussion_count`，正式金标准逐行保留 `annotator_flagged_ambiguous`；
- 每个项目人机 `kappa ≥ 0.70`；低于阈值时暂停该项目，复核规则并重新预标，不得
  删除难例来提高一致性；
- 人工标签与预标冲突时一律以人工标签为准；
- 正式金标准逐行保留 `label_source`（`HUMAN_VERIFIED` 或 `LLM_PRELABEL`）与
  `human_verified` 字段，论文须报告两者占比。

### 4bis.4 披露要求

论文的方法、威胁有效性与 AI 使用声明三处都必须写明：P02–P10 的边界参考标准是
"人工验证样本 + 模型预标"的混合标准，并给出模型标识、一致率、kappa 与人工验证
比例。不得声称"未使用生成式 AI 提供标注"。隐私与数据放行决定不适用本路径，
必须由获授权的人工审核员作出。

## 5. 双盲合并与仲裁

每个项目完成后，先运行：

```bash
python3 scripts/adjudicate_boundary_annotations.py \
  --annotator-a workbench/P02/annotation_review/boundary_annotator_a_reviewed.json \
  --annotator-b workbench/P02/annotation_review/boundary_annotator_b_reviewed.json \
  --merged-output workbench/P02/boundary_adjudication.json \
  --summary-output workbench/P02/boundary_agreement_summary.json
```

仲裁员只修改合并文件中的 `adjudication_label` 和 `adjudication_notes`。理由应指出
决定性语义关系，不能写“按模型结果”“效果更好”等结果导向理由。全部分歧解决后，
再次加入 `--final-output workbench/P02/candidate_breakpoints_adjudicated.json`。

每个项目必须报告样本数、标签分布、原始一致率、Cohen's kappa、分歧数和仲裁数。
`κ < 0.70` 时暂停该项目，复核规则并重新独立标注；`0.70 ≤ κ < 0.80` 时对各分层
误差做专项核查；`κ ≥ 0.80` 可进入常规仲裁。阈值只用于质量控制，不得删除难例来
人为提高一致性。

## 6. QA 人工审核

对每条 `qa_draft.json` 逐项检查：

1. 问题自然、明确，不依赖文件名、块编号或未说明的上下文。
2. `reference_answer` 能由指定证据直接支持，且不存在同样合理的冲突答案。
3. `gold_evidence_id` 指向正确父块；`gold_evidence_unit_id` 指向最小充分证据单元。
4. `gold_evidence_span` 是该证据单元中的最小充分原文，而非整页或整节。
5. 表格问题必须精确到唯一行；无法唯一对齐时拒绝或重写，不能保留模糊父表证据。
   原文逐字复用导致同一片段出现在多处时按第 6ter 节的等价证据集判定，此时要确认
   各等价位置的答案确实一致，而不是把不同事实并入同一题。
6. 问题类型、来源文档和章节正确；近重复问题不应同时大量保留。
7. 已完成第 7 节隐私审核。

通过时设置 `approval_status=APPROVED`；不自然、答案错误、证据不唯一或无法安全
脱敏时设置 `REJECTED`，并在 `review_notes` 写明原因。不得仅为凑足固定题数批准
低质量问题。项目最终题数可以少于 36，论文需报告各项目实际题数和题型分布。

## 6.1bis 题干自足性的机械判据（v1.3 增补）

第 6 节第 1 条要求题干不依赖文件名或块编号。第一轮人工终审对这一条的应用不一致：
题干以小节编号**代替**提问对象的被拒（如「3.2 能碳管理平台的建设描述了什么实现或
处理过程？」），而题干在一个本已自足的对象前**多带**一个小节编号前缀的被放行
（如「2.1.2.1 获取各点位的水位数据接口的请求路径是什么？」）。两者不是同一缺陷：
后者删掉编号后题干、答案与证据都不变。但该前缀是草稿生成器的残留，按第 6 节第 1
条的字面要求不应出现。

本节把这一条中无需判断的部分固定为机械判据：**题干不得以小节编号开头**。判据只认
「首段为 1–2 位数字、其后至少还有一个点分段」的形式，因此四位数年份不会命中，
`2025.7.22 召开的……` 与 `2025年11月10日……` 保持在范围内；紧跟量词的小数
（`1.5 秒`）也被排除。判据实现在 `scripts/qa_question_rules.py`，在
`scripts/import_qa_approval_workbook.py` 导入时应用。

命中行**不改写工作簿**。工作簿保留人工当时写下的判定，导入器把该行记为
`RULE_6_1` 排除并单独报告，因此「人工放行、规则排除」这一事实在产物里是可查的。
在本轮 225 条候选上判据命中 13 条，其中 10 条原为人工放行（P03 四条、P04 两条、
P05 四条），另 3 条本已因其他原因被拒。

**事后规范化的如实说明**：与第 4.1bis 节相同，本节判据是在第一轮终审完成、观察到
放行与拒绝口径不一致之后才写下的，不是事前规范。论文按此口径报告，并同时给出规则
排除前后的题数。

## 6bis. QA 内容预审（v1.1 增补）

QA 草稿在进入人工终审前先经过两道非授权性检查，二者都不产生批准状态：

1. 机械核对（`scripts/prepare_qa_prereview_batches.py`）：证据单元能否在语料中解析、
   证据片段是否被单元原文包含、单元是否属于声明的父块、参考答案文字是否出现在
   证据片段中、是否存在同题干的近重复记录。
2. LLM 内容预审（`scripts/merge_qa_prereview.py` 汇总）：对每条给出
   `content_verdict`（`APPROVED`／`NEEDS_REWRITE`／`REJECTED`）、难度建议和
   `suggested_privacy_flags`，并写明依据。

预审结果的 `approval_status` 与 `privacy_review_status` 一律保持 `PENDING`。
预审只是给人工审核员的排查线索，`APPROVED` 不等于入选，`REJECTED` 也不自动淘汰。
P01 的 27 条 QA 已完成人工内容审批，不进入本节预审，避免覆盖既有人工判断。

## 6ter. 多证据模型与候选集构建（v1.1 增补）

第 6 节第 5 条要求证据唯一。企业文档集不满足这个前提：同一项目的技术协议、方案
和需求规格逐字复用整段文字，同一句话合法地存在于多个文档，任何单一单元都无法被
论证为「那一处」证据。因此改为按等价证据集评估，并配套以下流程。

### 6ter.1 等价集解析

`scripts/resolve_qa_evidence_equivalents.py` 把语料中所有包含该证据片段的单元解析
为等价集，检索命中其中任一处即算正确。判定规则：

1. 片段少于 8 个字符不做扩展（`SPAN_TOO_SHORT`），因为短片段的重合不代表语义等价。
2. 表格行单元的匹配只用数据行内容，不含被复制到每行的表头上下文，否则一处证据会
   被误报为整张表都等价。
3. 出现次数超过 12 次判为 `TOO_MANY_OCCURRENCES`，不扩展为等价集。这类片段（例如
   一份规格里所有接口共用的请求方式）不是可区分证据，对应问题必须改写或排除。
   该阈值落在实测分布的空隙内：草稿的出现次数集中在 1–8，随后直接跳到 18 以上。

### 6ter.2 复议与补写

`NEEDS_REWRITE` 与因重复被 `REJECTED` 的草稿在多证据模型下复议，判定只取
`APPROVED_MULTI_EVIDENCE`、`REWRITE_PROPOSED`、`REJECTED` 三种。题干指定单一文档或
条款号的改为直接问事实本身；答案不完整的按所引片段补全；项目题数仍不足时补写草稿。

改写稿和补写稿在进入候选集前必须通过机械校验
（`scripts/merge_qa_readjudication.py`、`scripts/verify_supplementary_qa_drafts.py`）：
片段须命中真实单元、不超过出现次数上限、且能支持答案。支持性允许对表格行的改述，
但答案的每个实质片段都要能回溯到该片段；只允许「必填/是」这类已登记的同义改写。

### 6ter.3 表头可靠性标记

字符级支持性无法发现另一类错误：答案给某一列指派了表头并未声明的语义角色。当证据
所在表格的表头存在重复列名或占位列名（如整行都是「主档」）时，该候选标记
`needs_role_check=YES`，由人工终审确认列语义归属，不得因文字匹配通过而默认放行。

### 6ter.4 状态约束

候选集全部记录的 `approval_status` 与 `privacy_review_status` 仍为 `PENDING`，
`human_approved=False`。多证据放行、改写和补写都只改变候选状态，不产生批准。
候选集经 `scripts/build_qa_approval_workbook.py` 生成人工终审工作簿；论文须报告
最终人工保留的题数，而不是候选数。

### 6ter.5 终审录入方式

人工可直接编辑终审工作簿，也可用 `scripts/serve_qa_approval.py` 提供的本机终审界面。
后者只是同一工作簿同一四列的录入方式，逐条原子写回，不改动任何候选列，判定权与
判定口径不变；它额外做的只有一件事：把 `gold_evidence_span` 在语料中的每一处原文取出
并高亮，使第 6ter.1 节的等价性要求（各位置须支持同一答案）可以被实际核对，而不是
凭工作簿里被截断的 unit id 列表推断。录入端的校验规则与
`scripts/import_qa_approval_workbook.py` 同源，界面接受的判定必须也是导入门禁接受的
判定。

## 7. 隐私与企业敏感信息审查

每条 QA 和其公开证据按以下类别检查：

- `PERSON_NAME_OR_CONTACT`：姓名、手机号、邮箱、工号或可识别个人组合；
- `INTERNAL_NETWORK_OR_URL`：内网域名、IP、主机名、接口路径或环境地址；
- `SECRET_OR_CREDENTIAL`：口令、token、密钥、证书、连接串或可用认证参数；
- `CUSTOMER_OR_VENDOR_IDENTITY`：非公开客户、供应商、项目代号或合同主体；
- `SECURITY_ARCHITECTURE`：访问控制细节、漏洞、端口、拓扑或防护策略；
- `PROPRIETARY_BUSINESS_DATA`：金额、产能、人员、运营指标、合同条款等非公开数据。

处理顺序是：删除不必要字段；使用稳定占位符匿名化；必要时将答案改写为不泄密但
语义等价的形式；仍有风险则拒绝该题。任何真实 secret/credential 一律拒绝，不能
仅靠掩码保留可复原结构。

审核结果只能取：

- `NO_RISK`：没有识别到敏感项；
- `ANONYMIZED`：已脱敏，并在 `privacy_review_notes` 记录替换规则；
- `CLEARED`：存在已知标记但经授权可用于当前受控实验；
- `REJECTED`：不得进入正式 QA 或公开材料。

正式 QA 文件不得包含 `PENDING` 隐私状态。论文默认只公开统计量、方法代码和经过
脱敏的示例；原始企业文档及完整 QA 是否公开，取决于数据所有者的书面授权和所在
机构政策。没有授权时应发布字段规范、生成脚本和可复现实验协议，而不是原始语料。

### 7bis 隐私审查录入方式（v1.4 增补）

自动预筛（`scripts/prepare_qa_privacy_review.py`）把题干、参考答案、证据跨度、文档名
与章节路径**合并成一段文本后**匹配，因此 `privacy_flags` 只说明"命中了某类"，不说明
命中落在哪个字段。这一区分是判定的实质内容：只有题干、参考答案、证据跨度会随基准
发布，文档名与章节路径只留在工作台文件里。审核人必须按字段确认命中位置后再给结论，
不得仅凭标记类别整批放行或整批拒绝。

人工可直接编辑隐私审查工作簿，也可用 `scripts/serve_privacy_review.py` 提供的本机
复核界面。后者只是同一工作簿同两列的录入方式，逐条原子写回，判定权与本节口径不变；
它额外做的是按字段定位并高亮每一处敏感标识出现（标识由该项目语料文件名的最长公共
前缀推出，可用 `--identifier` 补充，界面列出实际匹配的标识以便复核），并挡住两处
跨脚本耦合：

1. 判 `REJECTED` 时同时把该行 `approval_status` 置为 `REJECTED`，因为
   `scripts/import_review_workbooks.py` 不接受"隐私拒绝而内容仍批准"的行；
2. 提示不得为脱敏而就地改写 `gold_evidence_span`——第 8 节的门禁要校验跨度（忽略
   空白后）逐字出现在语料单元内，就地改写会被判为"片段不在单元内"。需要脱敏时应
   改写参考答案并判 `ANONYMIZED`，在 `privacy_review_notes` 写明替换规则。

录入端校验规则与 `scripts/import_review_workbooks.py` 同源，界面接受的判定必须也是
导入门禁接受的判定。

**高亮只是辅助，不构成充分性保证。** 自动推导的标识来自本项目语料文件名的公共前缀，
只能覆盖"作为甲方出现在文件名里"的那一个主体。正文里出现的第三方主体推导不出来，
界面不会高亮。P01 实际执行中即有一例：QA020 因答案与证据跨度含关联企业名而被拒，
该名称不在自动推导的标识内，是审核人通读原文后识别的。因此审核人必须完整读过题干、
参考答案与证据跨度全文后再给结论，不得仅依据高亮为空就判 `NO_RISK`；发现新主体时应
用 `--identifier` 补充，使同批其余行也能被标出。

## 8. QA 门禁与审计产物

QA 审核完成后运行：

```bash
python3 scripts/finalize_qa_review.py \
  --corpus workbench/P02/corpus.json \
  --qa-review workbench/P02/annotation_review/qa_reviewed.json \
  --approved-output workbench/P02/qa_approved.json \
  --summary-output workbench/P02/qa_approval_summary.json \
  --require-ready
```

脚本会验证审批状态、父块、精确单元、证据片段包含关系和隐私放行状态。每个项目
最终应保留：A/B 原始盲标、合并文件、一致性摘要、已仲裁边界、审核后的 QA、QA
门禁摘要、语料 SHA-256 和脚本版本。原始盲标不得在仲裁后覆盖。

走第 6ter 节候选集路径的 P02–P10 改用审批簿导入，这是候选集进入正式 QA 文件的
唯一通道：

```bash
python3 scripts/import_qa_approval_workbook.py          # 只报告，不写文件
python3 scripts/import_qa_approval_workbook.py --write  # 全部行判定完成后才写
```

导入脚本的约束（任一不满足即拒绝写入该项目）：

1. 只读 `approval_status`、`privacy_review_status`、`difficulty`、`review_notes`
   四列；其余列与生成时的候选集逐格比对，被改动即报错中止，不做静默放行。
2. 任何一行未判定或只判了一半（内容与隐私必须同时填）时，整个项目不写文件。
3. `APPROVED` 行必须有难度；`REJECTED`（内容或隐私任一）必须在 `review_notes`
   写明理由；`ANONYMIZED` 必须写明替换规则；`needs_role_check=YES` 的行必须写明
   列语义已核对，否则视为未完成判定。
4. 证据字段不采信工作簿，一律按当前语料重新解析：片段必须仍能命中真实单元、
   不超过出现次数上限、且声明过的等价单元仍然包含该片段。
5. 只有内容 `APPROVED` 且隐私为 `NO_RISK`／`ANONYMIZED`／`CLEARED` 的记录写入
   `workbench/<project>/qa_approved.json`，并保留 `candidate_state`、
   `draft_origin`、`answer_grounding` 等来源信息。
6. 第 6.1bis 节判据命中的行不写入正式文件，即使工作簿判为 `APPROVED`；该行连同
   其工作簿判定一并记入 `rule_excluded`，并在导入摘要中打印。

## 8bis. 单轴退出的声明方式（v1.3 增补）

一个项目可能在某一条指标上不可用，而在另一条上完全成立。本轮出现了两例方向相反的
情况：P08 的边界一致率未达阈值，其 QA 无恙；P10 的题数在终审后低于下限，其边界
标注无恙。为此把「退出」定义为按轴声明，而不是整项目丢弃：

1. 退出必须写在 `configs/formal_run_manifest.json` 里，`boundary_withheld` 或
   `qa_withheld` 置真，并附对应的 `*_reason`。缺理由即为门禁阻塞。
2. 声明不能用来掩盖实际达标的项目：`audit_formal_readiness.py` 会反向校验——
   `boundary_withheld` 为真时边界金标准文件必须确实不存在，`qa_withheld` 为真时
   题数必须确实低于下限，否则报阻塞要求清除标记。
3. 退出只影响该轴的指标口径。`run_formal_suite.py` 对退出轴传入 `none`，
   `run_chunking_experiment.py` 据此跳过该轴的评测，产物中该轴为 `null` 且不写明细
   文件，CSV 对应列留空——留空而不是写 0，否则 0 会作为真实分数进入统计。
4. 退出项目的既有产物不销毁。P10 的 11 条已通过终审的问题仍保留在
   `qa_approved.json`，只是不参与检索指标；论文须报告其题数与退出原因。

## 9. 正式实验放行清单

只有同时满足下列条件，项目才能标为 `FORMAL_READY`：

- 300 个边界均有 A/B 独立标签且全部完成仲裁，或按第 8bis 节声明边界退出；
- 一致性统计和标签分布已记录，低一致性处置已完成；
- 所有保留 QA 均通过问题、答案、精确证据和隐私审核，且题数达到项目下限，或按
  第 8bis 节声明 QA 退出；
- 语料、金标准与配置的 SHA-256 已写入实验配置；
- P04–P10 未在配置锁定前泄露任何检索结果；
- 实验日志不含明文 secret、个人联系方式或未授权企业敏感信息。
