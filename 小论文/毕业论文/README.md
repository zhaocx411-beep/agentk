# 硕士学位论文《面向多智能体系统的动态语义切片算法优化研究》全套交付物总览

本目录存放北京邮电大学硕士学位论文**《面向多智能体系统的动态语义切片算法优化研究》**的完整大纲、中英文摘要、第 1 章至第 6 章全套正文、60 篇标准参考文献库、10 张全中文学术矢量图以及可复现实验脚本与数据表。全套论文正文字数合计超过 **7.2 万字**，远超工学硕士学位论文的篇幅与深度要求。

---

## 一、 全套论文章节文件清单（按装订顺序排列）

1. **中英文摘要、攻读学位期间学术成果与致谢（约 4,500 字）**：[摘要与致谢.md](file:///Users/apple/Desktop/毕业设计/小论文/毕业论文/摘要与致谢.md)
   - 包含中文摘要、英文 Abstract、关键词、IEEE ICAISG 2026 录用论文成果说明及致谢。
2. **论文总体大纲**：[大纲.md](file:///Users/apple/Desktop/毕业设计/小论文/毕业论文/大纲.md)
3. **第 1 章 绪论（已融合 2022–2026 深度文献调研 40 篇代表作，约 12,000 字）**：[第1章_绪论.md](file:///Users/apple/Desktop/毕业设计/小论文/毕业论文/第1章_绪论.md)
   - 涵盖 1.1 研究背景与意义、1.2 国内外研究现状（深度融合 60 篇文献及国内人大/北航/清华/复旦/智源/上海算法创新研究院团队成果，内置 **表 1-1 代表性方法与痛点对比矩阵**）、1.3 主要研究内容与双创新点、1.4 论文组织结构。
4. **第 2 章 相关理论与技术基础（深度扩充版，约 17,000 字，含式 2-1 至式 2-28 完整数学推导）**：[第2章_相关理论与技术基础.md](file:///Users/apple/Desktop/毕业设计/小论文/毕业论文/第2章_相关理论与技术基础.md)
   - 涵盖 2.1 检索增强生成与注意力头聚焦机理、2.2 十类主流及前沿文本切片方法数学建模（含 Late Chunking、Meta-Chunking PPL 极小值公式及 **表 2-1 综合特性对比表**）、2.3 多智能体系统（含 MemRL、HaluMem、**NativeScope 硬作用域级联误差上界**、Hua & Xiao 2026 可共享检索状态与多角色信息需求异构性）、2.4 评价指标与非参数聚类统计方法。
5. **第 3 章 结构与关键信息感知的自适应语义切片算法（第一核心创新点，约 20,000 字）**：[第3章_结构与关键信息感知的自适应语义切片算法.md](file:///Users/apple/Desktop/毕业设计/小论文/毕业论文/第3章_结构与关键信息感知的自适应语义切片算法.md)
   - 对应 IEEE ICAISG 2026 录用小论文深度扩写版，完整涵盖问题分析、携带出处的单元表示、四信号透明边界打分、文档自适应阈值 $\theta_D$、父块与表行子块构建、Algorithm 3-1/3-2 伪代码以及 10 个工业项目上的 15 种方法对比与消融实验。
6. **第 4 章 面向智能体角色的动态检索粒度选择（第二核心创新点，约 16,000 字）**：[第4章_面向智能体角色的动态检索粒度选择.md](file:///Users/apple/Desktop/毕业设计/小论文/毕业论文/第4章_面向智能体角色的动态检索粒度选择.md)
   - 完整涵盖多智能体角色与三类信息需求建模、基于 SEAC 单一索引的角色感知动态路由架构（MAS-DRG）、开发/验证集标定与锁定测试集盲测结果（Unit MRR 达 **$0.8416$**，6 胜 0 负，$p = 0.03125$；Hit@5 达 **$0.9600$**；平均上下文 Token 降低 **$35.8\%$**，较固定窗口降低 **$72.2\%$**）。
   - 配套实验数据详表：[第4章_实验结果与分析汇总.md](file:///Users/apple/Desktop/毕业设计/小论文/毕业论文/第4章_实验结果与分析汇总.md)
7. **第 5 章 多智能体详细设计文档生成系统中的应用（约 11,000 字）**：[第5章_多智能体详细设计文档生成系统中的应用.md](file:///Users/apple/Desktop/毕业设计/小论文/毕业论文/第5章_多智能体详细设计文档生成系统中的应用.md)
   - 涵盖 5.1 三层总体工程架构（`Multi-Agent-DetailedDesign` / `Multi-Agent-Framework` / `binary-tools`）、5.2 `Manager` 单例与 `session_create` / `session_query` 父子会话隔离机制、5.3 四级智能体编排与 Typst 编译工具链、5.4 可追溯 `rag_query` 检索服务接口契约设计、5.5 覆盖三层角色的 **5 个真实工业查询离线案例对比分析（表 5-1）** 与系统产出展示。
8. **第 6 章 总结与展望（约 3,800 字）**：[第6章_总结与展望.md](file:///Users/apple/Desktop/毕业设计/小论文/毕业论文/第6章_总结与展望.md)
   - 涵盖 6.1 全文工作总结与核心量化指标汇总、6.2 四项客观研究局限性剖析（语料覆盖、$K=6$ 簇级统计检验力、静态查表路由与离线评估边界）、6.3 四项未来研究方向展望。
9. **全篇统一编号参考文献库（GB/T 7714-2015 标准，共 60 篇）**：[参考文献_GB_T_7714_2015.md](file:///Users/apple/Desktop/毕业设计/小论文/毕业论文/参考文献_GB_T_7714_2015.md)

---

## 二、 全文配套学术矢量插图（`figures/*.svg`，共 10 张）

- **图 1-1**：[fig1_1_research_framework.svg](file:///Users/apple/Desktop/毕业设计/小论文/毕业论文/figures/fig1_1_research_framework.svg) —— 核心科学问题、三项研究内容与工程应用逻辑关系图
- **图 1-2**：[fig1_2_thesis_structure.svg](file:///Users/apple/Desktop/毕业设计/小论文/毕业论文/figures/fig1_2_thesis_structure.svg) —— 论文章节组织结构与技术路线图
- **图 2-1**：[fig2_1_rag_pipeline.svg](file:///Users/apple/Desktop/毕业设计/小论文/毕业论文/figures/fig2_1_rag_pipeline.svg) —— 检索增强生成三阶段基本流程与双塔稠密向量检索原理图
- **图 3-1**：[fig3_1_seac_pipeline.svg](file:///Users/apple/Desktop/毕业设计/小论文/毕业论文/figures/fig3_1_seac_pipeline.svg) —— SEAC 结构与关键信息感知自适应语义切片总体框架图
- **图 3-2**：[fig3_2_project_mrr.svg](file:///Users/apple/Desktop/毕业设计/小论文/毕业论文/figures/fig3_2_project_mrr.svg) —— 锁定测试集各工程项目 P04–P09 检索性能对比图
- **图 3-3**：[fig3_3_ablation_study.svg](file:///Users/apple/Desktop/毕业设计/小论文/毕业论文/figures/fig3_3_ablation_study.svg) —— SEAC 单组件移除消融实验对比图
- **图 4-0**：[fig4_0_mas_drg_architecture.svg](file:///Users/apple/Desktop/毕业设计/小论文/毕业论文/figures/fig4_0_mas_drg_architecture.svg) —— 面向智能体角色的单索引动态检索粒度选择架构图
- **图 4-1**：[fig4_1_question_type_mrr.svg](file:///Users/apple/Desktop/毕业设计/小论文/毕业论文/figures/fig4_1_question_type_mrr.svg) —— 不同智能体问题类型在各父子融合权重 $\alpha$ 下的检索精度对比图
- **图 4-2**：[fig4_2_routing_comparison.svg](file:///Users/apple/Desktop/毕业设计/小论文/毕业论文/figures/fig4_2_routing_comparison.svg) —— 全局固定配置与角色动态路由配置跨数据集对比图
- **图 4-3**：[fig4_3_token_accuracy_tradeoff.svg](file:///Users/apple/Desktop/毕业设计/小论文/毕业论文/figures/fig4_3_token_accuracy_tradeoff.svg) —— 不同策略的检索精度与上下文 Token 开销权衡图
- **图 5-1**：[fig5_1_system_architecture.svg](file:///Users/apple/Desktop/毕业设计/小论文/毕业论文/figures/fig5_1_system_architecture.svg) —— 多智能体详细设计文档自动生成系统三层总体架构与 SEAC + MAS-DRG 接入图
- **图 5-2**：[fig5_2_agent_orchestration.svg](file:///Users/apple/Desktop/毕业设计/小论文/毕业论文/figures/fig5_2_agent_orchestration.svg) —— 详细设计多智能体父子会话层级编排与可追溯 `rag_query` 动态粒度交互流程图

---

## 三、 可复现实验分析脚本与数据产物

- **第 4 章实验复现与制图脚本**：[generate_chapter4_artifacts.py](file:///Users/apple/Desktop/毕业设计/小论文/毕业论文/generate_chapter4_artifacts.py)
- **208 道有效工业问答角色映射与逐题检索/Token 明细表**：[chapter4_questions_role_mapping.csv](file:///Users/apple/Desktop/毕业设计/小论文/毕业论文/chapter4_results/chapter4_questions_role_mapping.csv)
- **第 4 章全部消融与路由实验汇总 JSON**：[chapter4_summary.json](file:///Users/apple/Desktop/毕业设计/小论文/毕业论文/chapter4_results/chapter4_summary.json)
