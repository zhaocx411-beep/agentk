# LLM 边界预标 v1

状态：**预标已完成，人工抽样验证未开始；不得作为正式金标准使用**。

协议版本 `industrial-seac-llm-prelabel-v1.0`，规则见
`../../docs/annotation_and_privacy_protocol.md` 第 4bis 节，实际判定口径见
`labeling_rules_applied.md`。

## 内容

- `batches/`：从 `boundary_blind_master.json` 抽取的 36 个批次（每批 75 条），
  只含标注员可见字段，不含任何方法预测。
- `labels/`：逐条预标签，字段为 `id`／`label`／`flag`／`note`。
- `boundary_llm_prelabel_P0X.json`：合并后的预标文件，每条带 `label_source=LLM_PRELABEL`、
  `human_verified=false`、`human_label=""`。
- `prelabel_summary.json`：分项目、分层的标签分布与输出哈希。
- `prelabel_consistency.{json,md}`：确定性规则冲突与分层分布离群审计。
- `batch_manifest.json`：输入文件哈希、批次划分与候选总数。

## 当前数据

P02–P10 共 2700 条候选：`BREAK` 455、`NO_BREAK` 1820、`NOISE` 425，其中 234 条标记
`NEEDS_HUMAN_REVIEW`。机械审计报出 2 条结构冲突（均因标题路径解析错乱，且均已转
人工复核），未标复核的冲突为 0。

## 下一步

预标签本身不是金标准。必须先由人工填写
`../prelabel_verification_v1/SEAC_boundary_prelabel_verification.xlsx`，再运行
`scripts/promote_llm_prelabels.py`；只有人机 `kappa ≥ 0.70` 且验证行齐全时才会写出
`workbench/P0X/candidate_breakpoints_adjudicated.json`。

## 保密

批次与预标文件含企业文档片段，仅限受控环境内部使用，不得作为论文附件、公开数据或
上传公开标注服务。
