# Research Implementation Plan

本文档给出两条可执行路线：

1. 中文核心/应用型期刊稳妥路线：继续使用当前 10 个企业项目文档集，完成 AutoSEAC 方法、边界金标、QA 金标和跨项目实验。
2. IP&M 类英文期刊冲刺路线：在 AutoSEAC 基础上升级到金融 RAG 场景，研究 structure-aware and evidence-grounded RAG for financial analysis documents。

两条路线不是互斥关系。推荐先完成路线 A，形成稳定论文；路线 B 可作为增强版、英文论文或后续工作。

---

## Version A: 中文核心/应用型期刊路线

### 1. 论文定位

建议题目方向：

- 面向企业项目文档问答的结构语义融合层级切分方法
- 面向 RAG 的复杂工程文档自适应切分方法研究
- 融合结构语义与自进化优化的企业项目文档切分方法

核心问题：

企业项目文档包含需求、设计、接口、数据库、会议纪要等多类文档，具有标题层级深、表格密集、接口参数密集、数据库 schema 密集、跨文档术语重复等特点。通用 fixed-size、recursive、semantic chunking 难以同时保持上下文完整性和检索粒度。

核心贡献：

1. 构建企业项目文档边界标注和 QA 检索评价集。
2. 提出结构语义融合的自适应父块切分方法 SEAC。
3. 提出父子层级检索机制，用子块提升命中精度，用父块保证上下文完整。
4. 引入 AutoSEAC 自进化 Skill，在受约束搜索空间中自动优化切分参数。
5. 在 10 个真实项目文档集上验证方法优于常见切分 baseline。

### 2. 数据设计

项目规模：

- P01-P10，共 10 个项目文档集。
- 每个项目尽量包含：需求规格书、详细设计、接口文档、数据库设计、会议纪要、调研报告。

数据划分：

- P01-P02：开发集 / AutoSEAC 搜索集。
- P03：验证集 / 版本选择集。
- P04-P10：最终测试集，只用于最终主实验。

边界标注：

- P01：全量边界标注，双人标注 + 仲裁，作为标注规范和开发金标。
- P02-P10：分层抽样标注，每项目 500-1200 个候选边界。
- 抽样维度：文档类型、边界类型、标题层级、表格/段落/heading transition、模型不确定样本。

QA 标注：

- 每项目最低 30 条，理想 50 条。
- 10 个项目总量目标：300-500 条 QA。
- QA 类型至少覆盖：功能说明、流程步骤、接口请求方式、接口参数、返回参数、数据库表描述、数据库字段、约束条件、数量/时长/比例、会议结论。

### 3. 方法实现

基础方法 SEAC：

1. 文档解析为 atomic units：heading、paragraph、table、table_row。
2. 对相邻 unit 计算 boundary score。
3. boundary score 包含：
   - semantic distance；
   - heading transition；
   - heading path change；
   - protected group transition；
   - key-term density change；
   - same-section continuity penalty；
   - same-table/protected-group penalty。
4. 使用 adaptive threshold 决定父块边界。
5. 对长表格、长段落进行结构化切分。
6. 为每个 chunk 注入文档类型、标题路径和核心主题。

层级检索方法：

1. parent chunk：保持语义上下文完整，用于最终返回。
2. child chunk：在 parent 内部按表格行、参数行、字段行或短文本片段拆分，用于精确检索。
3. 检索得分融合：
   - parent score 表示上下文整体相关性；
   - child score 表示局部证据命中能力；
   - final score = alpha * parent_score + (1 - alpha) * max_child_score。

AutoSEAC 自进化 Skill：

1. 编写 `SKILL.md`，规定方法内核、可调边界、禁止修改项、评价流程。
2. 编写 `autoseac_config.yaml`，保存可调参数。
3. 编写统一实验入口：读取配置、生成 chunk、评估 boundary 和 retrieval、写入版本目录。
4. 每轮只允许修改一个主要假设或一组同源参数。
5. 每轮保存：配置、结果、chunk 统计、改动说明、是否保留、失败原因。
6. 测试集不进入自动优化闭环。

### 4. Baseline

必须包含：

- fixed-size chunking；
- recursive character chunking；
- structure-only chunking；
- semantic chunking；
- proposed_v6 / SEAC 手工版本；
- AutoSEAC 自进化版本。

建议补充：

- LangChain RecursiveCharacterTextSplitter；
- Markdown/Header splitter；
- LlamaIndex SentenceSplitter 或 SemanticSplitter；
- parent-child retrieval baseline。

### 5. 实验指标

边界指标：

- Precision；
- Recall；
- F1；
- per-boundary-type F1；
- per-document-type F1。

检索指标：

- MRR；
- Hit@1；
- Hit@3；
- Hit@5；
- Recall@K；
- per-question-type performance。

效率指标：

- chunk_count；
- mean_chunk_tokens；
- retrieval_chunk_count；
- indexing time；
- query latency；
- token cost。

### 6. 消融实验

至少包含：

- w/o semantic distance；
- w/o structure score；
- w/o key-term density；
- w/o continuity penalty；
- w/o adaptive threshold；
- w/o hierarchical child chunks；
- w/o table-row handling；
- w/o heading path context。

目标是证明方法不是单纯调参，而是每个设计点都有贡献。

### 7. 误差分析

需要人工分析失败样本：

- 标题层级误导；
- 表格字段相似导致误召回；
- 父块正确但子块排序错误；
- 子块命中但父块上下文过大；
- 问题表述与文档术语不一致；
- 目录/封面/噪声误入检索；
- 多文档同名功能混淆。

### 8. 论文结果组织

主表：

- 各方法在 P04-P10 上的 Boundary F1、MRR、Hit@K。

补充表：

- P01 标注一致性；
- QA 审计结果；
- per-project 结果；
- per-question-type 结果；
- 消融实验；
- 效率对比。

论文叙事：

1. 通用 chunking 方法无法适配复杂工程文档。
2. 人工边界标注提供可靠评价尺子。
3. SEAC 通过结构语义融合获得更好的父块边界。
4. 层级子块提升检索粒度。
5. AutoSEAC 在受控搜索下进一步优化参数并保持可解释性。
6. 跨项目实验验证泛化。

### 9. 推荐时间表

第 1 周：

- 完成 P02-P10 corpus 构建；
- 生成 candidate breakpoints；
- 固定 P01 仲裁金标和 QA 审计结果。

第 2-3 周：

- P02-P10 分层抽样边界标注；
- 每项目准备 30-50 条 QA；
- QA 审计并形成 final QA。

第 4 周：

- 实现 AutoSEAC Skill、配置文件、版本目录和实验入口；
- 在 P01-P02 搜索。

第 5 周：

- 在 P03 验证并选择最终版本；
- 冻结最终 AutoSEAC。

第 6 周：

- 在 P04-P10 做最终测试；
- 跑 baseline、消融、误差分析。

第 7-8 周：

- 写论文实验、方法、数据集、分析章节。

---

## Version B: IP&M 类英文期刊金融 RAG 路线

### 1. 论文定位

建议英文题目方向：

- AutoSEAC: Self-Evolving Structure-Aware Chunking for Evidence-Grounded Financial RAG
- Structure-Aware and Evidence-Grounded Retrieval-Augmented Generation for Financial Analysis Documents
- Adaptive Hierarchical Chunking for Financial Document Question Answering

核心问题：

金融分析文档包括年报、公告、研报、财务报表、行业报告和市场数据说明。这类文档具有长文本、多表格、强数值、强时间属性、多公司对比、多期间对比和证据可审计要求。普通 chunking 方法容易切断表格上下文、丢失指标口径、混淆年份或公司，导致 RAG 回答缺乏事实性和证据可靠性。

核心贡献：

1. 将金融 RAG 的切分问题形式化为 evidence-grounded chunking problem。
2. 构建金融文档 QA benchmark，覆盖文本、表格、数值、时间和跨公司对比。
3. 提出 AutoSEAC-Fin，融合结构、语义、数值、时间和证据约束的自进化切分框架。
4. 在检索、证据引用和答案事实性三个层面验证方法。
5. 分析 chunking 对金融 RAG 中数值正确性、证据完整性和可审计性的影响。

### 2. 数据设计

数据来源：

- iFinD 数据可用于内部研究和实验构建，但必须确认授权范围。
- 论文可复现部分建议尽量使用公开数据：年报、公告、交易所披露文件、公开研报摘要、公开财务报表。
- 如果 iFinD 原始数据不能公开，应只公开统计信息、标注协议、脱敏样例和代码。

文档类型：

- 年度报告；
- 半年度报告；
- 季度报告；
- 公司公告；
- 行业研报；
- 财务指标表；
- 股票池或行业分类说明。

任务类型：

- 单公司单期间事实问答；
- 单公司多期间趋势问答；
- 多公司横向对比；
- 财务指标定义和口径问答；
- 表格字段定位；
- 数值计算；
- 风险因素归纳；
- 证据引用。

数据划分：

- Train/Search：部分年份、部分公司；
- Validation：不同公司或不同年份；
- Test：完全 held-out 公司和 held-out 时间段。

必须避免：

- 同一公司同一年报同时出现在搜索和测试；
- 测试集问题泄漏到 AutoSEAC 搜索过程；
- 使用未来数据回答历史问题；
- 让 Agent 看到测试集错误分析后继续优化。

### 3. 金融 QA 设计

QA 类型：

1. Fact QA：某公司某年营业收入是多少？
2. Table QA：资产负债表中某字段的数值是多少？
3. Temporal QA：某指标较上一年如何变化？
4. Comparative QA：A 公司和 B 公司谁的毛利率更高？
5. Calculation QA：根据收入和净利润计算净利率。
6. Evidence QA：答案依据年报哪一节或哪张表？
7. Risk QA：公司披露的主要风险因素是什么？
8. Definition QA：某财务指标的口径如何定义？

每条 QA 应包含：

- question；
- answer；
- source_document；
- evidence_block_id；
- evidence_span；
- evidence_table_id；
- company；
- year；
- metric；
- calculation_formula；
- question_type；
- answer_type；
- audit_status。

### 4. 方法升级：AutoSEAC-Fin

在 SEAC 基础上新增金融特征：

结构特征：

- section hierarchy；
- table title；
- row header；
- column header；
- footnote；
- unit/币种；
- report period。

数值特征：

- number density；
- financial metric terms；
- percentage / amount / growth rate；
- currency unit；
- same-row numeric context；
- table header alignment。

时间特征：

- year；
- quarter；
- reporting period；
- announcement date；
- historical vs forward-looking statements。

证据特征：

- table cell provenance；
- row/column header preservation；
- footnote preservation；
- source citation completeness；
- cross-chunk evidence continuity。

切分原则：

- 不拆散同一财务表的表头与关键行；
- 不拆散指标、数值、单位、期间；
- 对大表允许行级 child chunk，但 parent 必须保留表名、期间、公司、列名；
- 对跨期间表格保留年份列上下文；
- 对风险因素类长文本使用段落级 child chunk；
- 对公式计算类问题保留公式来源和输入数值来源。

### 5. 检索与回答评价

检索指标：

- MRR；
- Recall@K；
- nDCG@K；
- evidence recall；
- table evidence recall。

答案指标：

- exact match；
- numeric tolerance accuracy；
- unit correctness；
- period correctness；
- company correctness；
- citation correctness；
- faithfulness；
- answer with complete evidence。

金融特有错误类型：

- 年份错；
- 公司错；
- 指标口径错；
- 单位错；
- 百分比和小数混淆；
- 表头丢失；
- 使用未来数据；
- 只召回结论，未召回原始证据；
- 多公司对比时证据不完整。

### 6. Baseline

必须加入更强 baseline：

- fixed-size；
- recursive；
- semantic chunker；
- structure-only；
- table-aware chunker；
- LangChain splitter；
- LlamaIndex splitter；
- parent-child retriever；
- finance-specific table serialization baseline；
- no-chunk long-context baseline。

如果使用 LLM 回答，还需要比较：

- retrieve-only evidence recall；
- RAG answer accuracy；
- long-context direct answering；
- RAG with reranker；
- RAG with query decomposition。

### 7. AutoResearch / Skill 设计

AutoSEAC-Fin Skill 应包含：

- financial document assumptions；
- allowed data fields；
- forbidden leakage rules；
- allowed chunking parameters；
- table preservation rules；
- numeric evidence rules；
- train/validation/test permissions；
- version retention policy；
- error taxonomy；
- rollback policy。

版本保留条件：

- validation MRR 提升；
- evidence recall 不下降；
- numeric accuracy 不下降；
- chunk count 不异常膨胀；
- 在不同公司/年份上提升稳定；
- 错误分析不显示明显泄漏或过拟合。

测试集使用纪律：

- 测试集只在最终版本冻结后运行；
- 测试集结果不能驱动下一轮 AutoResearch；
- 若根据测试错误继续改方法，必须重新划分新的测试集。

### 8. IP&M 级实验要求

主实验：

- across companies；
- across years；
- across document types；
- across question types。

消融：

- w/o structure；
- w/o numeric feature；
- w/o temporal feature；
- w/o table preservation；
- w/o evidence citation constraint；
- w/o AutoResearch search；
- w/o hierarchical parent-child retrieval。

泛化实验：

- 在工程项目文档训练，在金融文档测试；
- 在金融文档训练，在工程项目文档测试；
- 或者用同一 AutoSEAC 框架分别搜索两个领域，比较稳定组件。

显著性检验：

- bootstrap confidence interval；
- paired significance test；
- per-company performance variance；
- per-year robustness。

### 9. 风险与对策

风险 1：iFinD 数据不可公开。

对策：

- 检查授权；
- 使用公开年报/公告构建可复现实验子集；
- iFinD 仅用于辅助构建或内部验证；
- 论文公开代码、协议、统计和脱敏样例。

风险 2：金融 RAG 变成另一个大题，拖慢毕业/投稿。

对策：

- 先做 small pilot；
- 仅作为外部验证加入中文论文；
- 如果效果强，再单独发展英文论文。

风险 3：数值 QA 标注成本高。

对策：

- 先做 100-200 条高质量金融 QA；
- 优先覆盖表格字段和简单计算；
- 不一开始做复杂多跳推理。

风险 4：方法被认为只是调参。

对策：

- 固定搜索空间；
- 保留可解释公式；
- 做消融；
- 报告失败版本；
- 用 held-out 公司和年份验证。

### 10. 推荐执行顺序

如果目标是 IP&M，不建议马上全量转向金融。推荐三步：

第一步：完成 Version A。

- 企业项目文档是主线。
- 得到稳定 AutoSEAC 方法和论文雏形。

第二步：金融 pilot。

- 选 20-50 份公开年报/公告/研报。
- 构建 100-200 条金融 QA。
- 比较 AutoSEAC 是否优于 baseline。

第三步：决定是否升级英文论文。

- 如果金融 pilot 明显提升，扩展到 100+ 文档、300+ QA。
- 如果提升不明显，保留金融作为外部验证，中文论文仍然成立。

---

## Final Recommendation

短期最稳路线：

继续推进 10 个企业项目文档，完成 AutoSEAC 中文论文。

中期增强路线：

加入金融文档 pilot，作为外部验证或第二实验场景。

长期英文路线：

如果金融 pilot 强，再将主题升级为 structure-aware and evidence-grounded RAG for financial analysis documents，冲击 IP&M 类英文期刊。

一句话：

不要现在推倒重来做金融 RAG；先把企业文档 AutoSEAC 做成稳定主线，再用金融场景证明方法具有外部迁移能力。这样风险最低，论文上限也更高。
