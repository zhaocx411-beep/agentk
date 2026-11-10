# 边界预标人工验证包 v1

状态：**待人工填写**。这是 P02–P10 边界金标准放行的唯一入口。

## 你需要做的

逐行填写 `human_label`、`review_status`、`review_notes` 三列，其余列不要改动。
**推荐用标注工具**，不要直接在 Excel 里填——表格行单元在单元格里会挤成一长串带重复
表头的文字，很难辨认：

```bash
python3 scripts/serve_boundary_verification.py
```

工具只监听本机，一次显示一条边界，把表头与数据行渲染成对齐的单元格并合并重复的
合并单元格列，键盘 `1`／`2`／`3` 打标后自动跳到下一条未判，每次打标即写回本工作簿，
可随时关掉、下次从第一条未判处继续。按 `?` 看判定口径与全部快捷键。工具不读取任何
预标文件，盲态与手填等价。仍然可以直接用 Excel 填，两种方式写的是同一个文件。

共 1050 行：

| 项目 | 一致率样本 | 待复核行 | 合计 |
|---|---:|---:|---:|
| P02 | 100 | 18 | 118 |
| P03 | 100 | 13 | 113 |
| P04 | 100 | 9 | 109 |
| P05 | 100 | 12 | 112 |
| P06 | 100 | 7 | 107 |
| P07 | 100 | 9 | 109 |
| P08 | 100 | 40 | 140 |
| P09 | 100 | 28 | 128 |
| P10 | 100 | 14 | 114 |

`row_group` 为 `AGREEMENT_SAMPLE` 的行是分层随机样本，只有它们参与人机一致率与
Cohen's kappa 的统计；`FLAGGED_REVIEW` 的行是预标阶段判不准的难例，必须判定但单独
报告，不计入一致率。

工作簿刻意不含 LLM 预标签、预标依据和任何检索结果。填写前请勿查阅
`../llm_prelabel_v1/labels/` 或 `boundary_llm_prelabel_*.json`，否则一致率失去意义。

判定规则见 `../../docs/annotation_and_privacy_protocol.md` 第 4 节：只允许
`BREAK`／`NO_BREAK`／`NOISE`，不得因文本接近 512 token 而判 `BREAK`，难例填最佳判断
并把 `review_status` 置为 `NEEDS_DISCUSSION`。

## 填完之后

```bash
python3 scripts/promote_llm_prelabels.py            # 只读门禁检查，输出一致率与 kappa
python3 scripts/promote_llm_prelabels.py --write    # 通过后写出正式金标准
```

门禁会拒绝：未修改的模板、缺行、多行、未完成或非法标签、以及任一项目
`kappa < 0.70`。人工标签与预标冲突时一律以人工标签为准。

`verification_manifest.json` 记录种子、抽样分布、样本 ID 与模板哈希。

## 保密

含企业文档片段，仅限受控环境内部使用，不得作为论文附件或上传公开标注平台。
