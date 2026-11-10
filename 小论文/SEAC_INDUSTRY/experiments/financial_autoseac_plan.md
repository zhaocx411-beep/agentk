# AutoSEAC-Fin Implementation Plan

本文档给出一条完整路线：将当前 RAG chunking 研究转向金融场景，形成一篇面向 IP&M 类英文期刊的论文雏形。

核心定位：

> AutoSEAC-Fin: a task-driven self-evolving chunking framework for evidence-grounded financial RAG.

基本策略：

- 公开金融 benchmark 用于保证可复现性和国际说服力。
- iFinD 数据用于真实金融业务场景验证。
- AutoSEAC-Fin 不以“人工切分一致”为唯一目标，而以 evidence retrieval、answer correctness、numeric accuracy 和 citation correctness 为主要优化目标。
- 人工边界或结构规则作为 regularization / diagnostic，不作为唯一终点。

---

## 1. 研究问题

### 1.1 背景

金融 RAG 需要在年报、公告、研报、财务报表、行情和指标数据中准确检索证据并生成可审计答案。普通 chunking 方法通常按固定 token、递归字符、标题或语义相似度切分，但金融文档存在以下难点：

- 表格密集，表头、行名、列名和数值必须共同保留；
- 指标名、数值、单位、期间和公司主体不可被切散；
- 多公司、多年份、多季度对比需要跨片段证据；
- 财务指标计算依赖多个输入数值和公式口径；
- 公告和研报具有明显时间敏感性；
- 金融问答要求 evidence citation、numeric accuracy 和 faithfulness。

因此，金融 RAG 的 chunking 不能只追求块长均匀，而应面向下游任务自动优化。

### 1.2 核心问题

本文研究：

> 如何构建一种任务导向的自进化 chunking 框架，使其在金融文档 RAG 中同时提升证据召回、答案正确性、数值准确性和引用可靠性？

### 1.3 论文贡献

建议写成四点：

1. We formulate chunking for financial RAG as a task-driven optimization problem, where chunk boundaries are optimized under retrieval, answer-level, numerical, and citation constraints.
2. We propose AutoSEAC-Fin, a self-evolving structure-aware chunking framework that searches chunking strategies under a controlled Skill protocol.
3. We introduce financial structure constraints to preserve company, period, metric, unit, table header, row/column context, and evidence provenance.
4. We evaluate AutoSEAC-Fin on public financial QA benchmarks and a real-world iFinD-based financial corpus, showing improvements over widely used chunking baselines.

---

## 2. 数据体系

### 2.1 数据分为两层

第一层：公开 benchmark。

用途：

- 提供可复现实验；
- 说服国际读者；
- 避免“私有数据不可验证”的质疑。

候选数据：

- FinanceBench：open-book financial QA，含问题、答案和 evidence strings。
- FinQA：金融数值推理 QA，含 reasoning program。
- FinDER：如果数据可获取，适合作为 retrieval-oriented RAG benchmark。

第二层：iFinD 真实金融场景数据。

用途：

- 体现真实金融业务复杂性；
- 验证方法在商业金融数据中的实际价值；
- 支撑 “real-world financial corpus validation”。

注意：

- 必须检查 iFinD 授权协议。
- 原始文档和原始数据未获许可前不要公开。
- 论文中公开统计、标注协议、代码和脱敏样例。

### 2.2 iFinD 语料建议范围

优先选择：

- 年报；
- 半年报；
- 季报；
- 公司公告；
- 行业研报；
- 个股研报；
- 财务指标表；
- 公司基本资料；
- 重大事件公告。

初始 pilot：

- 30-50 份文档；
- 100-200 条 QA。

正式实验：

- 100-300 份文档；
- 300-500 条 QA；
- 覆盖 30-100 家公司；
- 覆盖 3-5 年；
- 覆盖多个行业。

### 2.3 数据划分

必须避免测试泄漏。

建议划分：

- Search/Train：部分公司 + 部分年份，用于 AutoSEAC-Fin 搜索。
- Validation：不同公司或不同年份，用于版本选择。
- Test：held-out companies + held-out years，只用于最终测试。

例子：

| Split | 用途 | 例子 |
| --- | --- | --- |
| Train/Search | 自动搜索参数和策略 | 2021-2023 年，A 股部分公司 |
| Validation | 选择最终版本 | 2024 年，未出现在 train 的公司 |
| Test | 最终报告 | 2025 年，held-out 公司和行业 |

测试集纪律：

- 测试集不能参与 AutoResearch/AutoSEAC 优化；
- 不能根据测试集错误继续调参；
- 如果必须根据测试错误改方法，需要重新划分新的 test set。

---

## 3. QA 构建方案

### 3.1 QA 类型

金融 QA 必须覆盖不同能力。

| 类型 | 目标 | 示例 |
| --- | --- | --- |
| Fact QA | 单一事实定位 | 某公司 2024 年主营业务是什么？ |
| Table QA | 表格字段定位 | 某公司 2024 年营业收入是多少？ |
| Temporal QA | 时间变化 | 2024 年净利润相比 2023 年变化多少？ |
| Comparative QA | 多公司/多指标比较 | A 公司和 B 公司谁的毛利率更高？ |
| Calculation QA | 简单公式计算 | 根据收入和净利润计算净利率。 |
| Evidence QA | 证据定位 | 该结论来自哪份公告或哪张表？ |
| Risk QA | 风险因素抽取 | 公司披露的主要经营风险是什么？ |
| Definition QA | 指标口径 | 扣非净利润的含义是什么？ |
| Event QA | 公告事件 | 公司公告的本次回购金额上限是多少？ |
| Forecast QA | 研报预测 | 研报预测公司下一年度 EPS 是多少？ |

### 3.2 每条 QA 的字段

建议 JSON schema：

```json
{
  "qa_id": "FIN_QA_0001",
  "question": "",
  "answer": "",
  "answer_type": "numeric/fact/list/comparison/calculation",
  "question_type": "table/temporal/comparative/calculation/risk/definition/event/forecast",
  "company": "",
  "ticker": "",
  "industry": "",
  "period": "",
  "source_type": "annual_report/research_report/announcement/financial_table",
  "source_document": "",
  "gold_evidence_id": "",
  "gold_evidence_span": "",
  "gold_table_id": "",
  "gold_row_header": "",
  "gold_column_header": "",
  "metric": "",
  "unit": "",
  "calculation_formula": "",
  "supporting_evidence_ids": [],
  "audit_status": "verified",
  "audit_notes": ""
}
```

### 3.3 QA 质量控制

每条 QA 必须满足：

- 答案可由文档直接推出；
- evidence span 明确；
- 数值题必须记录单位和期间；
- 计算题必须记录公式和输入证据；
- 多公司对比题必须记录两个或多个证据；
- 答案不能依赖模型常识；
- 问题不能泄露 evidence 文本的完整句子；
- 不使用测试集问题调参。

QA 审计标签：

- KEEP：可直接用于实验；
- FIX：证据或问题需要修正；
- DROP：证据不充分或答案无法验证。

建议至少 10%-20% QA 双人复核，报告一致率。

---

## 4. 文档解析与 Chunk 单元

### 4.1 金融文档解析

需要解析为 atomic units：

- heading；
- paragraph；
- table；
- table_row；
- table_cell；
- footnote；
- caption；
- formula；
- metadata block。

每个 unit 附带 metadata：

- company；
- ticker；
- report_date；
- fiscal_year；
- fiscal_quarter；
- document_type；
- section_path；
- table_title；
- row_header；
- column_header；
- unit；
- currency；
- metric；
- source_page；
- source_location。

### 4.2 表格序列化

金融表格不能简单转为纯文本。

推荐格式：

```text
文档: 2024 Annual Report
公司: XXX
期间: 2024
表名: Consolidated Income Statement
单位: RMB million
行: Revenue
列: 2024
值: 12345
```

对于 table_row child chunk：

- 必须包含表名；
- 必须包含公司；
- 必须包含期间；
- 必须包含单位；
- 必须包含列名；
- 必须包含行名；
- 必须包含原始值。

### 4.3 噪声过滤

过滤或弱化：

- 页眉页脚；
- 免责声明；
- 目录；
- 重复版权声明；
- 页码；
- 无信息表格线；
- 扫描 OCR 噪声。

但注意：

- 研报免责声明通常不用于 QA；
- 年报中的会计政策、风险提示可能是有效内容，不能误删。

---

## 5. AutoSEAC-Fin 方法

### 5.1 总体流程

1. 文档解析为 units。
2. 构建结构增强文本表示。
3. 计算相邻 units 的 boundary score。
4. 根据 adaptive threshold 生成 parent chunks。
5. 在 parent 内生成 child chunks。
6. 检索时融合 parent score 和 child score。
7. 用 evidence retrieval、answer correctness、numeric accuracy 和 citation correctness 评价。
8. AutoSEAC-Fin 根据验证集结果自动调整配置。

### 5.2 Boundary score

候选边界得分：

```text
S(i) =
  w_sem * semantic_distance(u_i, u_{i+1})
+ w_struct * structural_transition(u_i, u_{i+1})
+ w_num * numeric_context_shift(u_i, u_{i+1})
+ w_time * temporal_context_shift(u_i, u_{i+1})
+ w_table * table_boundary_signal(u_i, u_{i+1})
- w_cont * continuity_penalty(u_i, u_{i+1})
```

其中：

- semantic_distance：语义向量距离；
- structural_transition：标题、章节、表格结构变化；
- numeric_context_shift：指标/单位/数值密度变化；
- temporal_context_shift：年份/季度/报告期变化；
- table_boundary_signal：表名、行列头、表格组变化；
- continuity_penalty：同公司、同期间、同表、同指标上下文连续时惩罚断开。

### 5.3 Parent chunk

作用：

- 保持完整上下文；
- 用于最终答案生成；
- 用于 citation；
- 避免 child 过碎导致证据不足。

Parent chunk 规则：

- 不超过 max_parent_tokens；
- 尽量不跨公司；
- 尽量不跨报告期；
- 尽量不跨大章节；
- 同一核心表格的标题、单位、表头和重要行应在同一 parent 或可追溯 parent 内。

### 5.4 Child chunk

作用：

- 精确检索证据；
- 适配表格行、指标行、公告事件句。

Child chunk 类型：

- paragraph child；
- table row child；
- table cell with context child；
- formula child；
- risk bullet child；
- event sentence child。

Child chunk 必须注入 metadata：

- company；
- period；
- metric；
- unit；
- table_title；
- row_header；
- column_header；
- parent_chunk_id。

### 5.5 检索融合

父子融合：

```text
score(parent) =
  alpha * sim(query, parent)
+ (1 - alpha) * max_child_sim(query, child in parent)
```

金融增强：

```text
score(parent) += beta_company * company_match
score(parent) += beta_period * period_match
score(parent) += beta_metric * metric_match
score(parent) -= penalty_wrong_period
score(parent) -= penalty_wrong_company
```

### 5.6 AutoSEAC-Fin 优化目标

建议目标函数：

```text
Objective =
  0.30 * EvidenceRecall@5
+ 0.20 * MRR
+ 0.20 * AnswerAccuracy
+ 0.15 * NumericAccuracy
+ 0.10 * CitationCorrectness
- 0.05 * CostPenalty
```

可选加入：

```text
+ 0.05 * BoundaryRegularization
```

BoundaryRegularization 可以来自：

- 少量人工边界标注；
- 表格结构约束；
- 不跨公司/期间规则；
- 不切散 row/column/header/unit/metric。

### 5.7 可调参数

配置文件建议包含：

```yaml
semantic_weight: 0.35
structure_weight: 0.25
numeric_weight: 0.15
temporal_weight: 0.10
table_weight: 0.15
continuity_penalty: 0.40
breakpoint_percentile: 0.80
parent_max_tokens: 512
parent_soft_target_tokens: 320
child_target_tokens: 96
child_max_tokens: 160
hierarchy_parent_weight: 0.35
company_match_boost: 0.10
period_match_boost: 0.10
metric_match_boost: 0.08
wrong_company_penalty: 0.20
wrong_period_penalty: 0.20
table_row_child_enabled: true
cell_context_enabled: true
footnote_context_enabled: true
```

---

## 6. AutoResearch / Skill 协议

### 6.1 Skill 文件

建立：

- `skills/autoseac_fin/SKILL.md`
- `configs/autoseac_fin.yaml`
- `scripts/run_autoseac_fin.py`
- `experiments/autoseac_fin_versions/`

SKILL.md 内容：

1. 方法内核；
2. 金融文档约束；
3. 可调参数边界；
4. 禁止修改项；
5. 每轮实验步骤；
6. 保留/回滚标准；
7. 数据划分权限；
8. 版本记录格式；
9. 失败归因模板。

### 6.2 版本目录

每轮版本保存：

```text
experiments/autoseac_fin_versions/v0001/
  config.yaml
  hypothesis.md
  results_train.json
  results_validation.json
  retrieval_details.json
  answer_details.json
  error_analysis.md
  decision.json
```

decision.json：

```json
{
  "version": "v0001",
  "hypothesis": "",
  "changed_parameters": {},
  "validation_score": 0.0,
  "previous_best_score": 0.0,
  "decision": "keep/reject",
  "reason": ""
}
```

### 6.3 每轮迭代纪律

每轮只允许：

- 调整一个主要假设；
- 或调整一组同源参数。

不允许：

- 同时大改多个方向；
- 使用 test set；
- 修改 gold answer；
- 修改评测脚本以提高分数；
- 删除失败样本；
- 根据测试结果继续优化。

---

## 7. 实验设计

### 7.1 Baseline

必须包含：

- fixed-size chunking；
- recursive chunking；
- semantic chunking；
- structure-only chunking；
- table-aware chunking；
- LangChain RecursiveCharacterTextSplitter；
- LlamaIndex SentenceSplitter / SemanticSplitter；
- parent-child retrieval baseline；
- long-context baseline；
- AutoSEAC；
- AutoSEAC-Fin。

### 7.2 主实验

主表建议包含：

| Method | Evidence Recall@5 | MRR | Answer Acc | Numeric Acc | Citation Correct | Cost |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |

分别报告：

- public benchmark；
- iFinD private corpus；
- combined average。

### 7.3 消融实验

至少包含：

- w/o semantic score；
- w/o structure score；
- w/o numeric context；
- w/o temporal context；
- w/o table preservation；
- w/o company/period/metric boost；
- w/o parent-child retrieval；
- w/o AutoResearch search；
- w/o citation constraint。

### 7.4 鲁棒性实验

按以下维度拆分：

- by company；
- by industry；
- by year；
- by document type；
- by question type；
- by answer type；
- by table vs text evidence；
- by single-hop vs multi-hop evidence。

### 7.5 统计显著性

建议：

- bootstrap confidence interval；
- paired test；
- per-question paired comparison；
- effect size；
- version trajectory plot。

---

## 8. 答案生成与评测

### 8.1 RAG pipeline

流程：

1. Query parsing：抽取公司、期间、指标。
2. Retrieval：用不同 chunking 方法检索 top-k。
3. Context construction：将 top-k chunks 拼成上下文。
4. Answer generation：LLM 生成答案和引用。
5. Evaluation：与 gold answer 和 gold evidence 对比。

### 8.2 Answer evaluation

数值题：

- exact match；
- tolerance match；
- unit match；
- period match；
- formula input match。

文本题：

- exact/fuzzy match；
- LLM judge；
- evidence-supportedness；
- citation correctness。

引用：

- retrieved chunk contains gold evidence；
- generated citation points to retrieved evidence；
- evidence span covers answer。

### 8.3 LLM judge 注意事项

如果使用 LLM judge：

- 固定评测 prompt；
- 使用 blind evaluation；
- 抽样人工复核；
- 报告 judge-human agreement；
- 不让 judge 看到方法名。

---

## 9. 实施步骤

### Phase 0: 资产盘点

任务：

- 确认 iFinD 数据授权；
- 列出可用文档类型；
- 确认是否能导出全文、表格、公告、研报；
- 确认公开 benchmark 下载方式；
- 确定可公开内容范围。

产物：

- `docs/data_inventory.md`
- `docs/ifind_license_notes.md`

### Phase 1: Public benchmark baseline

任务：

- 下载 FinanceBench / FinQA；
- 转换为统一 QA schema；
- 构建文档 corpus；
- 跑 fixed/recursive/semantic/structure baseline；
- 跑当前 AutoSEAC。

产物：

- `workbench/financebench/corpus.json`
- `workbench/financebench/qa.json`
- `experiments/financebench_baselines/`

### Phase 2: iFinD pilot

任务：

- 选择 30-50 份文档；
- 构建 100-200 条 QA；
- 做 QA 审计；
- 跑 baseline；
- 判断 AutoSEAC 是否有明显收益。

产物：

- `workbench/ifind_pilot/corpus.json`
- `workbench/ifind_pilot/qa_audited.json`
- `experiments/ifind_pilot_results/`

### Phase 3: AutoSEAC-Fin

任务：

- 实现金融 metadata；
- 实现 table/cell/row serialization；
- 实现 numeric/temporal/table boundary features；
- 实现 parent-child 金融增强检索；
- 实现 AutoResearch version loop。

产物：

- `seac_chunking/finance_units.py`
- `seac_chunking/finance_chunkers.py`
- `scripts/run_autoseac_fin.py`
- `configs/autoseac_fin.yaml`

### Phase 4: Full iFinD corpus

任务：

- 扩展到 100-300 份文档；
- 构建 300-500 条 QA；
- 划分 train/validation/test；
- 双人复核部分 QA；
- 冻结 final test。

产物：

- `workbench/ifind_finance/corpus.json`
- `workbench/ifind_finance/qa_train.json`
- `workbench/ifind_finance/qa_validation.json`
- `workbench/ifind_finance/qa_test.json`

### Phase 5: Final experiments

任务：

- 跑全部 baseline；
- 跑 AutoSEAC；
- 跑 AutoSEAC-Fin；
- 跑消融；
- 跑鲁棒性；
- 跑显著性；
- 做误差分析。

产物：

- `experiments/finance_final/results.json`
- `experiments/finance_final/results.csv`
- `experiments/finance_final/ablation.csv`
- `experiments/finance_final/error_analysis.md`

### Phase 6: Paper writing

章节：

1. Introduction；
2. Related Work；
3. Task Formulation；
4. AutoSEAC-Fin Method；
5. Experimental Setup；
6. Results；
7. Ablation and Analysis；
8. Error Analysis；
9. Limitations and Ethics；
10. Conclusion。

---

## 10. 风险控制

### 10.1 数据授权风险

风险：

- iFinD 原始数据可能不能公开。

对策：

- 主实验保留公开 benchmark；
- iFinD 作为 private real-world validation；
- 只公开统计和脱敏样例；
- 论文清楚说明数据许可限制。

### 10.2 过拟合风险

风险：

- AutoSEAC-Fin 优化到验证集很高，但测试集不稳定。

对策：

- held-out company；
- held-out year；
- held-out document type；
- 版本保留只看 validation；
- test set 只运行最终版本。

### 10.3 LLM judge 风险

风险：

- 答案评价受 judge 模型偏差影响。

对策：

- 数值题优先用规则评测；
- 文本题 LLM judge + 人工抽检；
- 报告 judge-human agreement。

### 10.4 方法被认为只是调参

风险：

- 审稿人认为 AutoSEAC-Fin 只是自动调参数。

对策：

- 给出明确 problem formulation；
- 固定搜索空间；
- 给出可解释 boundary score；
- 做消融；
- 报告失败版本和版本轨迹；
- 证明金融结构特征是必要的。

---

## 11. 推荐时间表

第 1 周：

- 确认 iFinD 授权；
- 下载公开 benchmark；
- 统一 QA schema；
- 跑 public benchmark 初始 baseline。

第 2 周：

- 构建 iFinD pilot；
- 准备 100-200 条 QA；
- 做 QA 审计；
- 跑初始 AutoSEAC。

第 3-4 周：

- 实现 AutoSEAC-Fin 特征和 chunker；
- 建立 Skill 和 version loop；
- 在 train/validation 上自动搜索。

第 5 周：

- 扩展 iFinD 语料和 QA；
- 冻结 full test；
- 跑主实验。

第 6 周：

- 跑消融、鲁棒性、显著性；
- 做误差分析。

第 7-8 周：

- 写英文论文初稿；
- 补实验图表；
- 完成 reproducibility package。

---

## 12. 当前项目已有内容如何处理

已有工业项目文档工作不必删除。

可作为：

- 方法开发经验；
- AutoSEAC 初始版本来源；
- 中文论文备用线；
- 附录中的 non-financial external validation；
- 后续第二篇论文。

但如果本文完全转向金融，则主论文不强行放工业项目文档，避免叙事分散。

---

## 13. 最终建议

短期执行：

1. 先跑 FinanceBench/FinQA baseline。
2. 同步构建 iFinD pilot。
3. 用公开 benchmark 保证可复现性。
4. 用 iFinD 证明真实金融场景价值。
5. 在此基础上实现 AutoSEAC-Fin。

论文主线：

> Financial RAG needs task-driven chunking rather than fixed or purely semantic chunking. AutoSEAC-Fin uses a controlled self-evolving Skill to optimize chunking strategies with evidence retrieval, answer correctness, numerical accuracy, and citation correctness as feedback signals.

一句话：

用公开金融 benchmark 建立可信度，用 iFinD 数据建立真实业务价值，用 AutoSEAC-Fin 建立方法创新。
