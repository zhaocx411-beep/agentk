# AutoSEAC-Fin 实验协议

**版本：** 1.0  
**最后更新：** 2026-06-18  
**状态：** 官方实验协议，在 AutoResearch 开始前冻结

---

## 1. 概述

### 1.1 本文档目的

本文档定义了 AutoSEAC-Fin 的完整实验协议：将 AutoSEAC（自动语义证据感知分块）应用于金融领域的多跳问答。所有实验必须与本文档保持一致。偏离本协议的操作将不作为正式论文结果报告。

### 1.2 协议在方法开发之前

本协议中的所有设计决策均先于任何 AutoSEAC-Fin 实现或实验结果。此顺序确保方法选择不会受到已观察到的结果的影响。

### 1.3 核心主张

**AutoSEAC-Fin 能提升财务多跳问答的检索质量，且不牺牲泛化性。**

我们将核心主张分解为以下可检验的假设：

- **H1：** 语义边界检测能准确识别文档与表格中的财务语义单元边界，其效果优于固定大小滑动窗口。
- **H2：** 结合公司、期间和指标的元数据感知重排序，能提升财务问答的检索质量。
- **H3：** 在多个财务问答基准上的性能提升不依赖于特定数据集，且不牺牲泛化性。

## 2. 数据集

### 2.1 主数据集

| 数据集 | 类型 | 语言 | 主要领域 | 问题数 | 官方划分 |
| --- | --- | --- | --- | --- | --- |
| DocFinQA | 长文档问答 | 英文 | 美国上市公司年报 | ~1,500 | 有 |
| TAT-QA | 混合表格与文本问答 | 英文 | 金融报告混合 | ~16,000 | 有 |
| FinanceBench | RAG 基准 | 英文 | 美国上市财务 | ~10,000 | 有公开标签 |
| iFinD | 中文财务问答 | 中文 | 中国上市公司年报 | ~4,500 | 无公开划分 |

### 2.2 诊断数据集

| 数据集 | 诊断用途 | 语言 |
| --- | --- | --- |
| FinQA | 数值推理诊断 | 英文 |
| MultiHiertt | 表格层级推理诊断 | 英文 |

诊断数据集仅用于分析，**不**用于版本选择。

### 2.3 排除的数据集

以下数据集在本阶段明确排除：

- **ConvFinQA：** 对话式；将语义边界检测增益与对话消解效应分离存在困难。留待后续工作。
- **DCQA：** 文档集合；当前的 AutoSEAC-Fin 设计面向单文档/单报告检索。集合级检索留待后续工作。
- **通用表格问答数据集（如 WTQ、TabFact、FeTaQA）：** 缺乏财务语义背景。留待后续泛化性工作。

## 3. 语料库与问答模式

### 3.1 语料库模式

每个数据集都必须转换为标准语料库模式，以确保 AutoSEAC-Fin 能一致地接收输入。

**文档级模式：**

```json
{
  "corpus_id": "",
  "dataset": "",
  "company": "",
  "ticker": "",
  "fiscal_year": "",
  "fiscal_period": "",
  "period_start": "",
  "period_end": "",
  "document_type": "",
  "source_path": "",
  "language": "",
  "text": "",
  "tables": [],
  "metadata": {}
}
```

**表格模式：**

```json
{
  "table_id": "",
  "table_title": "",
  "table_caption": "",
  "num_rows": 0,
  "num_columns": 0,
  "column_headers": [],
  "row_headers": [],
  "cells": [],
  "footnotes": [],
  "unit": "",
  "is_hierarchical": false
}
```

**表格单元格模式：**

```json
{
  "cell_id": "",
  "row_index": 0,
  "column_index": 0,
  "row_header": "",
  "column_header": "",
  "value": "",
  "unit": "",
  "is_derived": false,
  "derivation_formula": "",
  "footnote_ids": [],
  "format": "",
  "numeric_value": null
}
```

### 3.2 问答模式

所有数据集必须映射到统一的问答模式。

**主模式：**

```json
{
  "qa_id": "",
  "dataset": "",
  "question": "",
  "answer": "",
  "answer_type": "",
  "corpus_ids": [],
  "gold_evidence_ids": [],
  "gold_table_ids": [],
  "gold_row_headers": [],
  "gold_column_headers": [],
  "calculation_formula": "",
  "requires_calculation": false,
  "requires_multi_hop": false,
  "num_hops": 1,
  "language": ""
}
```

**证据模式：**

```json
{
  "evidence_id": "",
  "qa_id": "",
  "corpus_id": "",
  "table_id": "",
  "start_offset": 0,
  "end_offset": 0,
  "evidence_span": "",
  "evidence_type": "",
  "row_header": "",
  "column_header": "",
  "unit": "",
  "numeric_value": null,
  "supporting_evidence_ids": [],
  "is_multi_evidence": false,
  "relation_type": ""
}
```

**答案类型：**

| 类型 | 描述 |
| --- | --- |
| `number` | 单一数值答案 |
| `number_list` | 数值列表答案 |
| `text` | 自由文本答案 |
| `boolean` | 是/否，正确/错误 |
| `table` | 表格结构答案 |
| `template` | 结构化/模板化答案 |
| `multi_span` | 多个文本片段 |

**问题类别：**

| 类别 | 描述 | 示例 |
| --- | --- | --- |
| `single_evidence` | 单一证据定位 | "2023 年收入是多少？" |
| `multi_evidence` | 跨文档/跨段落的多证据 | "收入与 COGS 各是多少？" |
| `calculation` | 需要算术公式 | "计算毛利率。" |
| `comparison` | 比较两个或多个值 | "2022 年与 2023 年收入的差异。" |
| `table_cell` | 表格单元格定位 | "资产表中的总资产是多少？" |
| `free_text` | 开放式推理 | "收入下降的原因是什么？" |

**样本条目：**

```json
{
  "qa_id": "docfinqa_train_00001",
  "dataset": "DocFinQA",
  "question": "2023 年总资产是多少？",
  "answer": "142,431",
  "answer_type": "number",
  "corpus_ids": ["docfinqa_corpus_00001"],
  "gold_evidence_ids": ["docfinqa_evidence_train_00001_0"],
  "gold_table_ids": ["docfinqa_table_00001_1"],
  "gold_row_headers": ["总资产"],
  "gold_column_headers": ["2023"],
  "calculation_formula": "",
  "requires_calculation": false,
  "requires_multi_hop": false,
  "num_hops": 1,
  "language": "en"
}
```

### 3.3 财务单元分词

`finance_units.py` 模块定义了从表格语料库中提取的规范财务单元。每个单元都是一个可以包含在检索块中的带类型、带时间的可寻址原子。可识别的单元类型包括：

| 单元类型 | 描述 |
| --- | --- |
| `company` | 公司名称、代码 |
| `period` | 财年、季度、期间 |
| `metric` | 财务指标名称（收入、资产、毛利率等） |
| `metric_with_value` | 指标 + 数值 + 单位 |
| `table_reference` | 表格 ID、标题、脚注引用 |
| `calculation` | 公式或推导关系 |
| `comparison` | 显式财务比较 |
| `note` | 文本脚注、附注 |
| `context_block` | 文本段落中的连续上下文 |

**样本条目：**

```json
{
  "unit_id": "",
  "unit_type": "",
  "corpus_id": "",
  "table_id": "",
  "row_header": "",
  "column_header": "",
  "company": "",
  "ticker": "",
  "period": "",
  "metric": "",
  "unit": "",
  "source_document": "",
  "gold_evidence_id": "",
  "gold_evidence_span": "",
  "supporting_evidence_ids": [],
  "gold_table_id": "",
  "gold_row_header": "",
  "gold_column_header": "",
  "calculation_formula": "",
  "calculation_inputs": [],
  "audit_status": "verified",
  "audit_notes": ""
}
```

对于多证据和计算类问题，只要源数据集提供了充分的证据标注，就应填 `supporting_evidence_ids`。

## 4. 数据集划分

本项目使用四种实验划分角色：

| 划分角色 | 用途 | 可否影响 AutoResearch？ |
| --- | --- | --- |
| 搜索/训练 | 拟合编码器、搜索分块参数、运行探索性假设 | 是 |
| 验证 | 决定 AutoSEAC-Fin 版本的去留 | 是，仅通过冻结指标 |
| 公开测试 | 最终基准报告 | 否 |
| 外部测试 | 一次性最终泛化性检查 | 否 |

### 4.1 公开基准划分策略

**DocFinQA：**

- 如可用，使用官方划分。
- 搜索/训练可使用官方训练划分。
- 验证可使用官方开发/验证划分。
- 测试必须保留到最后选定版本冻结后。

**TAT-QA：**

- 如可用，使用官方训练/开发/测试划分。
- 若隐藏的官方测试集不可访问，使用公开开发集作为验证，并按文档从训练划分中创建可复现的保留划分。

**FinanceBench：**

- 使用公开可用的划分。
- 此数据集必须保持为外部保留测试——其结构与其他财务问答语料库有实质差异。

**iFinD：**

- 数据为私有；不能假设有官方划分。
- 创建按公司划分的保留划分：80% 训练/验证，20% 测试。
- 如果任何公司同时出现在训练/验证和测试中，则相应的 QA 对可能同时出现在两侧。

## 5. 评估框架

### 5.1 检索度量

检索在两个级别上评估：

**块级（检索效果）：**

| 度量 | 定义 |
| --- | --- |
| Recall@K | 前 K 个块中包含至少一条黄金证据的 QA 对比例 |
| Precision@K | 前 K 个块中是黄金证据的比例 |
| MRR | 第一个黄金证据的倒数排名均值 |
| NDCG@K | 归一化折损累积增益 |
| Evidence Coverage@K | 在前 K 个块中覆盖的黄金证据比例 |

**语料库级（索引质量）：**

| 度量 | 定义 |
| --- | --- |
| Avg Chunk Size | 块大小均值 |
| Std Chunk Size | 块大小标准差 |
| Evidence Split Rate | 被拆分到两个以上块的黄金证据比例 |
| Chunk Purity | 一个块中属于同一财务单元的原子比例 |

### 5.2 端到端问答评估

检索后，下游问答按以下方式评估：

| 度量 | 适用答案类型 | 定义 |
| --- | --- | --- |
| Exact Match | 数字、布尔 | 字符串精确匹配 |
| Numeric Tolerance | 数字 | 相对误差 ≤ 1% |
| F1 | 文本、多片段、模板 | 词汇 F1 |
| Table EM | 表格 | 所有单元格精确匹配 |
| Table Cell F1 | 表格 | 平均单元格级 F1 |

### 5.3 目标函数

版本选择的主目标函数为：

```
Obj = 0.25 * R@10 + 0.15 * R@20 + 0.10 * MRR + 0.15 * NDCG@10
    + 0.15 * EvidenceCoverage@20 + 0.10 * (1 - EvidenceSplitRate)
    + 0.10 * ChunkPurity
```

约束条件：AverageChunkSize ≤ 400 token，EvidenceSplitRate ≤ 0.20。

## 6. 基线

### 6.1 分块基线

| 基线 | 描述 |
| --- | --- |
| Fixed-Size Sliding | 固定 token 滑动窗口，窗口 = 200/400/600，步长 = 窗口/2 |
| Paragraph-Based | 按段落边界分块 |
| Recursive Structure | LangChain 递归字符文本分割器 |
| Sentence Window | 按句子分割，带前后 N 句上下文窗口 |
| Section-Based | 按文档节标题边界分块 |
| Table-Aware Naive | 每表一块，每节一块 |

### 6.2 检索基线

| 基线 | 描述 |
| --- | --- |
| BM25 Sparse | 稀疏词袋检索 |
| Dense Base | 冻结密集编码器（BGE-large-en, E5-mistral） |
| Dense Fine-Tuned | 在搜索/训练划分上微调的密集编码器 |
| HyDE | 假设文档嵌入 |
| Metadata Filter | 先检索再按公司/期间过滤 |
| Parent-Child | 小尺寸子块检索 + 较大父块返回 |

### 6.3 端到端基线

| 基线 | 描述 |
| --- | --- |
| Pipeline A | 分块 → 检索 → 阅读器回答 |
| Pipeline B | 分块 → 检索 → 重排序 → 阅读器回答 |
| Pipeline C | 分块 → 检索 → 重排序 → 多证据聚合 → 阅读器回答 |

## 7. AutoSEAC-Fin 架构

### 7.1 分块组件

**边界检测器：** 对语义边界进行评分并选择分割点。

- 输入：带嵌入的财务单元序列
- 输出：边界得分序列
- 分割：在局部极大值且得分 > `breakpoint_percentile` 处选择

**子块生成器：**

- 输入：两个分割点间的财务单元
- 目标尺寸：根据问题和检索类型可调
- 结果：一个或多个语义连贯的子块
- 约束：跨财务单元边界的子块绝不能分割单元格

**父块生成器：**

- 输入：连续的财务单元块
- 目标尺寸：用于 LLM 阅读器上下文窗口
- 结果：父块 = 连续的语义子块
- 约束：单一父块最多覆盖一项证据

### 7.2 检索组件

**财务重排序器：** 对检索到的子块重新排序，优先考虑：

- 与问题公司/代码相关的公司；
- 与问题时期相关的期间；
- 与问题指标相关的指标。

**约束感知组合器：** 执行硬约束：

- 一个子块的对应项不应与另一子块冲突；
- 应满足所需跨度数；
- 用于计算问题的项应数值一致。

### 7.3 财务序列化合约

表格证据不得仅序列化为原始行文本。行或单元格子项应包含显式的财务上下文：

```text
Company: <公司名>
Ticker: <代码>
Period: <期间>
Document: <文档>
Table: <表格标题>
Unit: <单位>
Row: <行头>
Column: <列头>
Value: <值>
```

渲染文本可根据最终语料库语言进行调整，但必须包含相同信息。

## 8. 边界得分规范

首个 AutoSEAC-Fin 边界得分应为：

```text
S(i) =
  w_sem * semantic_distance(u_i, u_{i+1})
+ w_struct * structural_transition(u_i, u_{i+1})
+ w_num * numeric_context_shift(u_i, u_{i+1})
+ w_time * temporal_context_shift(u_i, u_{i+1})
+ w_table * table_boundary_signal(u_i, u_{i+1})
- w_cont * continuity_signal(u_i, u_{i+1})
```

初始特征定义：

- `semantic_distance`：相邻单元嵌入距离。
- `structural_transition`：标题、节、表格或文档结构转换。
- `numeric_context_shift`：数值密度、指标、单位、货币或公式上下文的变化。
- `temporal_context_shift`：财年、季度、报告期间或日期的变化。
- `table_boundary_signal`：表格开始/结束、表格 ID 变化、表格标题变化、表头转换、行/列组变化。
- `continuity_signal`：同一公司、同一时期、同一表格、同一指标、同一公式或同一章节的连续性。

硬约束应在需要时覆盖软得分：

- 绝不在渲染的子表示中将表格单元格与其行/列/单位上下文分开；
- 避免父块跨不同公司；
- 避免父块跨不同财期，除非源文档显式对比它们；
- 如果某个分割点会破坏公式的输入-输出关系，则不在其中间分割。

## 9. 实验矩阵

### 9.1 主表

对于每个数据集（DocFinQA, TAT-QA, FinanceBench），按以下配置运行：

| ID | 分块方法 | 检索器 | 重排序器 | 目标 |
| --- | --- | --- | --- | --- |
| E1 | Fixed-Size (400) | BM25 | 无 | 下界 |
| E2 | Fixed-Size (400) | Dense Base | 无 | 简单密集下界 |
| E3 | Fixed-Size (400) | Dense Fine-Tuned | 无 | 强密集基线 |
| E4 | Paragraph-Based | Dense Base | 无 | 结构基线 |
| E5 | Section-Based | Dense Base | 无 | 文档结构基线 |
| E6 | Table-Aware Naive | Dense Base | 无 | 朴素表格基线 |
| E7 | AutoSEAC-Fin | Dense Base | 无 | 仅分块增益 |
| E8 | AutoSEAC-Fin | Dense Base | 财务重排序器 | 分块 + 重排序 |
| E9 | AutoSEAC-Fin | Dense Fine-Tuned | 财务重排序器 | 完整流水线 |

### 9.2 主表外

| ID | 条件 | 问题 |
| --- | --- | --- |
| X1 | 如 E7 > E5 且 p < 0.05 | SEAC 优于节边界？ |
| X2 | 如 E8 > E7 | 重排序独立增量？ |
| X3 | 如 E9 > E4 | 相对段落的端到端提升？ |
| X4 | 始终 | 诊断数据集分析 |
| X5 | 始终 | iFinD 单一运行 |

### 9.3 诊断分析

| 分析 | 数据集 | 指标 |
| --- | --- | --- |
| Numerical Reasoning | FinQA | Numeric Tolerance，按操作类型 |
| Table Hierarchy | MultiHiertt | 按层级深度的 Cell F1 |
| Evidence Split Rate | 全部 | EvidenceSplitRate，按块尺寸 |
| Chunk Size Distribution | 全部 | 均值、标准差、百分位 |
| Ablation on Weight w_* | DocFinQA | 每个权重归零时的 R@10 变化 |

## 10. 统计严谨性

### 10.1 假设检验

对于主结果表：

- 如果样本为配对（同一 QA 对、两种方法），使用配对 t 检验；
- 使用 Bonferroni 校正对每个数据集每次比较的 p 值进行校正；
- 报告效应量（Cohen's d）；
- 报告 95% 置信区间。

### 10.2 显著性阈值

| 上下文 | α 水平 |
| --- | --- |
| 主表比较 | 0.05 / 比较次数 |
| 诊断分析 | 0.05 |
| 探索性质问 | 报告 p 值，不宣称显著性 |

### 10.3 多重比较

如果探索 > N 次比较（N = 主表行数），以 `p < 0.01` 报告并以 "探索性证据" 标注。所有声称显著的结果必须指定：检验方式、样本量、效应量和校正策略。

## 11. 计算预算

| 阶段 | 最大估算 |
| --- | --- |
| 语料库构建 | 200 CPU 小时 |
| 每数据集编码 | 24 GPU 小时 |
| 每数据集微调 | 72 GPU 小时 |
| 每版本 AutoResearch | 300 CPU 小时 + 48 GPU 小时 |
| AutoResearch 总版本数 | 12 |
| 外部测试运行 | 每数据集 24 GPU 小时 |
| **合计** | ~1200 CPU 小时 + ~600 GPU 小时 |

若总成本超出估计 25% 以上，则在继续之前重新评估预算。

## 12. AutoResearch 纪律

### 12.1 审查防火墙

以下信息在 AutoResearch 搜索周期内**不得**影响系统：

- 黄金证据文件；
- 公开测试文件；
- 外部测试文件；
- iFinD 测试文件；
- 运行期间的评估代码；
- 运行期间的数据集转换输出；
- 运行期间的目标函数；
- 隐藏测试访问策略；
- 基线实现（不能削弱它们）。

### 12.2 单一假设规则

每个版本应仅改变以下之一：

- 一个主要假设；或
- 一组连贯的同族参数。

有效变更示例：

- 增大 `table_weight` 并减小 `breakpoint_percentile`，以保留财务表格上下文；
- 仅调整子块目标/最大尺寸；
- 仅调整公司/期间/指标重排序提升；
- 仅启用或禁用脚注上下文注入。

无效变更示例：

- 在同一版本中同时变更表格序列化、检索重排序和目标权重；
- 在结果不佳后编辑验证数据；
- 删除困难样本；
- 在发现分数下降后更改评估器逻辑。

### 12.3 版本目录

每个 AutoResearch 版本必须生成：

```text
experiments/autoseac_fin_versions/v0001/
  config.yaml
  hypothesis.md
  results_train.json
  results_validation.json
  retrieval_details.json
  error_analysis.md
  decision.json
```

`decision.json` 文件必须遵循：

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

### 12.4 保留/拒绝规则

版本可在以下条件下保留：

- 验证目标优于之前最佳；且
- 无主约束违反程度实质性增加；且
- 成本未超出声明容忍度。

版本在以下条件下必须拒绝：

- 验证目标劣于之前最佳；且
- 差异不可归因于噪声；且
- 无合理的边界情况说服力。

## 13. 报告清单

论文必须报告：

### 13.1 主结果

- 表：每数据集、每配置的检索指标（含置信区间）
- 表：每数据集、每配置的端到端指标
- 表：与现有工作的对比
- 图：每数据集的检索 Pareto 曲线
- 图：块尺寸分布

### 13.2 AutoSEAC-Fin 分析

- 表：边界得分权重的消融实验
- 图：AutoResearch 中版本的验证目标轨迹
- 表：保留版本的最终权重
- 表：硬约束违反的分布
- 图：子块尺寸与检索召回率的关系

### 13.3 诊断

- 表：FinQA 数值推理诊断（含分块影响）
- 表：MultiHiertt 表格层级诊断（含深度分析）
- 表：每个数据集的证据分割率
- 图：块纯度直方图
- 表：iFinD 结果（私有语料库，仅统计量）

### 13.4 统计报告

- 所有统计检验结果
- 效应量
- 置信区间
- 多重比较校正

### 13.5 失败与局限分析

- 每个基线在哪些 QA 对上失败
- AutoSEAC-Fin 边界检测何时失败
- 重排序何时降低召回率
- 失败是否与特定问题类型、表格类型或文档结构相关

## 14. 发布与复现

### 14.1 代码

- 所有代码公开，遵循 MIT 许可证。
- 仓库包含：语料库构建、QA 模式映射、`finance_units.py`、分块、检索、重排序、评估和 AutoResearch 控制器。
- 仓库不包含：黄金证据文件、测试集文件、iFinD 原始数据或任何私有语料库。

### 14.2 数据

- 公开基准：提供脚本，从官方来源下载并转换为规范语料库/QA 模式。
- iFinD：不重新发布原始数据。提供转换示例格式，附带私有语料库的描述性统计量，并说明数据获取方式（如可说明）。

### 14.3 模型

- 所有检索器检查点公开。
- 所有边界得分权重在配置文件中声明。
- 所有 AutoResearch 版本轨迹随仓库附带。

## 15. 里程碑

**里程碑 1：语料库与 QA 模式。**

- 所有数据集均已下载。
- 规范语料库与 QA 模式已定义。
- `finance_units.py` 通过模式级单元测试。

**里程碑 2：基线。**

- 所有分块基线实现并运行。
- 仅检索评估（无阅读器）完成。
- 存储所有基线结果。

**里程碑 3：AutoResearch。**

- AutoSEAC-Fin 实现。
- 运行受控训练/验证搜索。
- 保存版本轨迹。

**里程碑 4：外部公开测试。**

- 冻结最终 AutoSEAC-Fin 配置。
- 将 FinanceBench 作为外部保留公开测试运行一次。
- 不根据 FinanceBench 结果进行调参。

**里程碑 5：诊断。**

- 运行 FinQA 数值诊断。
- 运行 MultiHiertt 表格层级诊断。
- 运行消融实验。

**里程碑 6：iFinD 验证。**

- 确认许可证。
- 构建 iFinD 试点。
- 冻结 iFinD 测试。
- 运行最终选定版本。
- 如允许，报告私有语料库统计量和匿名示例。

## 16. 风险控制

### 16.1 测试泄露

**风险：** AutoResearch 过拟合到公开或 iFinD 测试集。

**控制：**

- 仅搜索/训练和验证可影响版本决策；
- 测试集仅最终使用；
- 所有版本决策必须引用验证指标；
- 测试失败不能触发参数编辑，除非创建新测试集。

### 16.2 元数据优势

**风险：** AutoSEAC-Fin 因接收更丰富的元数据而优于基线。

**控制：**

- 报告仅块检索；
- 报告元数据感知检索（所有方法使用相同重排序器）；
- 清晰区分分块增益与重排序增益。

### 16.3 数据集过广

**风险：** 过多基准稀释论文叙事。

**控制：**

- 主论文聚焦 DocFinQA、TAT-QA、FinanceBench 和 iFinD；
- FinQA 和 MultiHiertt 为诊断；
- 较新的智能体或宽泛基准留待后续工作，除非完全集成。

### 16.4 LLM 评分偏差

**风险：** 答案评估依赖评分模型的偏好。

**控制：**

- 尽可能使用基于规则的数值评估；
- 仅对自由文本答案使用盲法 LLM 评分；
- 抽样 10%-20% 进行人工审核；
- 报告评分-人工一致性。

### 16.5 私有数据许可

**风险：** iFinD 数据无法公开发布。

**控制：**

- 以公开基准作为可复现核心；
- 将 iFinD 报告为私有真实世界验证；
- 仅发布统计量、协议、代码，并在允许时发布匿名示例。

## 17. 当前建议的首要行动

下一步实现步骤为：

```text
TAT-QA -> 规范语料库/QA 模式 -> finance_units.py -> AutoSEAC-Fin 分块器 -> 仅检索评估
```

在此单数据集流水线稳定并产生可信的检索指标之前，**不要**启动 AutoResearch 参数搜索。

