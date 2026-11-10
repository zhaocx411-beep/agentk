# EasyChair 投稿元数据

## 会议与专题

- Conference：CCF ChinaSoft 2026
- Track / Special Issue：智能化工业软件工程：大模型驱动的开发、保障与治理
- Paper type：原创研究论文
- 截稿日期：2026-09-01

## 中文题名

SEAC：面向大模型工业软件工程知识检索的来源可追溯结构保持分块方法

## English title

SEAC: Provenance-Aware Structure-Preserving Chunking for Large-Model Retrieval over Industrial Software Engineering Documents

## 中文摘要

工业软件需求规格、接口说明、数据库设计、技术协议和会议纪要同时包含层级标题、约束语句与大规模表格。固定窗口或通用递归分块容易切断表头与数据行、标题与正文以及结论与其来源，导致大模型虽然检索到语义相似文本，却无法给出可核验的工程证据。本文提出来源可追溯的结构保持分块方法 SEAC。该方法把段落、标题和表格行规范化为带稳定标识的最小单元，联合相邻单元语义距离、结构转移和工程关键信息密度变化构造父块，并以保留表头和标题路径的细粒度子块触发父块返回，从而分离“用于匹配的粒度”与“用于解释的上下文”。在冻结协议下，本文对 10 个企业项目的 89 份文档进行评估，共包含 31841 个解析块和 66981 个可追溯单元；按预先声明的轴向排除规则，检索推断使用 6 个独立项目的 125 个问题。完整 SEAC 的单元平均倒数排名为 0.7937、前 5 命中率为 0.9520，强于最优非 SEAC 基线的 0.7290；相对预先指定的 LlamaIndex 比较器，平均倒数排名提高 0.1003，95% 置信区间为 [0.0042, 0.2008]，但项目级精确双侧检验为 p=0.0625，尚不能据此宣称一般统计优越性。移除表格行子块后平均倒数排名降至 0.6853，而移除父子融合后升至 0.8289。结果表明，当前证据最支持“带表头的行级检索”，并提示完整评分设计仍需在新项目上预注册复验。该方法以最优平面基线 5.50 倍的向量数量换取细粒度定位与来源审计能力，可为大模型驱动的工业软件需求分析、规范核对和责任追踪提供可验证的知识检索底座。

## Keywords（如系统要求逐条填写）

1. 检索增强生成 / retrieval-augmented generation
2. 文档分块 / document chunking
3. 工业软件工程 / industrial software engineering
4. 来源追溯 / provenance
5. 结构化文档 / structured documents
6. 质量治理 / quality governance

## Topics（如系统提供自定义主题）

- Large language models for software engineering
- Retrieval-augmented generation
- Industrial software engineering
- Document intelligence and chunking
- Traceability, auditability, and quality governance

## Authors

按 `作者信息待补充.md` 和 `author_metadata.json` 补齐。EasyChair 中的姓名顺序、英文拼写、邮箱、单位顺序必须和最终 PDF 完全一致。

## 可选的 Comments to Chairs

本文投稿至“智能化工业软件工程：大模型驱动的开发、保障与治理”专题。论文研究面向企业工业软件文档的大模型检索底座，重点讨论结构保持分块、来源可追溯、质量审计与治理。由于原始企业文档含有保密信息，稿件公开了非数据依赖的实现、冻结配置、合成示例、实验协议、聚合结果与哈希审计要求，但不公开可反推客户或项目身份的原文。若录用，作者将按会议要求现场报告。

## AI use disclosure（论文内已包含）

本研究使用 OpenAI Codex 辅助论文结构调整、中英文语言起草、图表代码生成与一致性检查。作者对相关工作、实验协议、数据处理、统计方法、全部数值、图表和最终表述进行了人工核验，并对研究和投稿决定承担全部责任。生成式人工智能未被列为作者，未决定论文署名、隐私开放、正式标注或研究结论。

