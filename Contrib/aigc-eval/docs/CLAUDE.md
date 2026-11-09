# CLAUDE.md

本文件给在本仓库工作的 AI/协作者提供导航。用大模型看生成视频、对照提示词，给出可核查、可审计的质量评测，并支持 Kling vs Seedance 成对对比与多候选横向排名。

> **⚠️ 本文整体停留在 `src/aigc_eval/` 包化重构之前，不要照着它找文件。**
> 它写的 `app.py`、`judge_pipeline.py`、`gemini_client.py`、`run_vertex.sh`、
> 「仓库没有 pyproject.toml」「零第三方依赖」都已经不成立：代码现在是
> `src/aigc_eval/` 包 + `worker/`，用 uv 管依赖，模型通道只剩 `agentk` 一条
> （Gemini 与 Keye 直连均已删除）。
>
> **当前状态以 README.md 为准。** 本文保留是因为它对判定逻辑、权重表、维度表的
> 描述仍有参考价值，那部分尚未逐条复核。

> 文档里的数字曾经对着代码核过。改代码时若动到本文列出的常量/开关/维度表，请同步改这里——上一版 CLAUDE.md 因为没同步，把权重表、叶子数、环境开关全写错了。

## 运行

```bash
python3 app.py                 # 需要 Python 3.12+（report_html.py 用了 PEP 701 f-string）
./run_vertex.sh                # Vertex 直连（注意脚本里写死了本机 python3.12 路径）
```

- 监听 `0.0.0.0:9900`（`app.py:638`，端口可用 `VIDEO_JUDGE_PORT` 覆盖）→ http://127.0.0.1:9900
- **无鉴权、监听全网卡、接受任意文件上传**，只适合可信内网。
- **零第三方 Python 依赖**：主链路纯标准库 + 系统 `ffmpeg`/`ffprobe`（需支持 psnr/ssim/blackdetect/freezedetect/tblend/signalstats）。唯一例外是 `eval_f1.py` 惰性 `import pandas`。仓库**没有** requirements.txt / pyproject.toml。
- 不要引入 numpy/opencv——抽帧、运动分析全部用 ffmpeg 表达式实现。
- 前端无构建、无框架、无 npm：`static/*.html` + 原生 JS。

## 设计哲学（重要）

**hybrid 架构**：固定 workflow 骨架 + 少数环节的受限 agent 循环。三条底线不可破坏：

1. **可审计**：每次模型调用的 system prompt / 输入 / 原始输出 / 解析 JSON 全量落盘（`trace_store.py`）。
2. **确定性拦截**：黑帧/时长/加权/一票否决/硬门控都由**程序**算，不信任模型自评分。「出现关键问题必须拦住，高画质分不能抵消」。
3. **缓存**：每个 `(case, source, engine, mode, CACHE_VERSION)` 的完整报告落盘 `runtime/results/*.json`，命中不再调模型。改了报告结构就 **bump `CACHE_VERSION`**（`judge_pipeline.py:3693`，现 `v41`），旧缓存自动失效。listwise 有独立版本 `LISTWISE_CACHE_VERSION`（`listwise.py:31`，现 `l1`）。

设计文档：`AGENT_DESIGN.md`、`WORKFLOW_AGENT_LOGIC.md`、`P0三维度_分析逻辑与提示词.md`。

## Pipeline 七步（`judge_pipeline.py:3727 analyze_case`）

阶段标题就是 UI 上看到的那串（`1 · 视频文件检查` … `7 · 生成报告`）。

| 步 | 谁做 | 位置 |
|---|---|---|
| 0 缓存命中 | 程序 | `:3787` 命中直接 return，一次模型都不调 |
| 1 视频文件检查 | 程序 | `integrity.inspect_video` + `check_integrity`：ffprobe 元信息 + blackdetect/freezedetect + 帧数校验。**前置 sanity 门控，不是加权维度** |
| 2 抽关键画面 | 程序 | `integrity.sample_frames_motion_aware`（依赖 `motion_profile`）运动感知采样，动得最快处加密；失败回退 uniform。再 `build_contact_sheet` 拼联系表 |
| 3 拆解提示词 | 模型 | `compile_prompt_vlm` 主用 / `compile_prompt_heuristic`(`:627`) 回退，输出可核对约束 checklist |
| 4 观察器 | 模型 | `GeminiObserver`(`:1290`)：整段视频原生输入，只描述事实 + 初判 + `focus_intervals` + 音画同步初判 |
| **4b 合并扫描** | 模型 | **默认主路径**：`GeminiZoomInspector.scan_unified`(`:3073`)，一次调用判完 一致性/指令遵循/崩坏/画面异常/文字/故事逻辑 |
| 4b′ 条件触发高清复核 | 模型 | `GeminiTargetedRechecker`(`:1443`)：仅对初判有风险且可定位的疑点抽原生帧复核一次（`TARGETED_RECHECK`，默认开，≤4 帧） |
| 5 验证器 | 模型 | `GeminiVerifier`(`:3472`) 纯文本，基于事实独立裁决每条约束 + 逐叶 rubric；与观察器初判交叉佐证（agree/disagree） |
| 6 汇总评分 | 程序 | `rubric.normalize_rubric` → `_override_leaf`(`:4448`) → `scoring.py` 全套 |
| 6′ 问题定位 + 证据 | 模型 + 程序 | `GeminiGroundingLocator`(`:1475`) 只补 bbox，**跑在打分之后、不参与判定**；`evidence.build_evidence` 配原生帧证据（可画红框） |
| 7 派生 + 报告 | 程序 | `annotation.annotate_report` → `build_cost` → `report_html.write_report_html` → trace 落盘 → 结果缓存 |

### 4b 的三条路径（务必分清）

- **合并扫描**（`UNIFIED_SCAN`，默认开）：1~2 次调用。整段视频 + 至多 `UNIFIED_SCAN_FRAMES=16` 张带镜头号帧（长边 ≤1280）。
- **分维度路径**（`JUDGE_UNIFIED_SCAN=0` 或合并扫描失败且 `UNIFIED_FALLBACK` 开）：`scan_consistency` / `scan_instruction` / `scan_visual` / `scan_text` / `scan_collapse_chunked` + 区域裁剪放大 + 崩坏成对比对，**约 37 次调用、~5 倍成本**。这条路是给本地 Keye 留的，注释已明确「已不作保证」。改动不要默认它还能跑对。
- **`GeminiZoomInspector.inspect` 的多轮 agent 循环已死**：`SKIP_ZOOM = True` 写死（`judge_pipeline.py:112`），`:3979` 那个分支永不执行。代码还在，别以为它生效。

### 写死为常量、env 已失效的开关（老文档在这里骗了很久）

`judge_pipeline.py:112-118`：

```python
SKIP_ZOOM = True            # 4b zoom agent 循环永久关闭
COLLAPSE_VOTES = 1          # 崩坏投票只跑 1 次，不再是多数表决
COLLAPSE_ADAPTIVE = False   # :3433 的自适应加投恒不触发
ADAPTIVE_AUDIO = False      # 4c 音频独立复听死路径，AUDIO_RECHECK_SYSTEM 闲置
```

`JUDGE_SKIP_ZOOM` / `JUDGE_COLLAPSE_VOTES` / `JUDGE_ADAPTIVE_AUDIO` 这几个环境变量**不再有任何作用**。要恢复得改代码。

## 评测维度（`rubric.py:RUBRIC`，**16 个叶子**）

叶子按 `dim` 归入加权维度：`instruction` 7 个、`consistency` 3 个、`visual` 2 个、`collapse` 2 个、`text` 1 个、`story` 1 个（不加权）。

| dim | 叶子 |
|---|---|
| `consistency` | `consistency_input_reference`、`consistency_multishot_continuity`、`consistency_temporal_drift` |
| `instruction` | `instruction_basic_content`、`instruction_multishot`、`instruction_camera`、`instruction_audio`、`audio_sync`、`prompt_spatiotemporal`、`prompt_environment` |
| `collapse` | `dyn_collapse_major`、`dyn_physics` |
| `visual` | `visual_exposure_color`、`visual_greasy`（均 `hard=False`，观感项） |
| `text` | `visual_text` |
| `story` | `story_logic_coherence`（**有意不加权**，见下） |

`normalize_rubric` 会做程序化适用性兜底：无图参考 → `consistency_input_reference` 记 `not_applicable`；`shot_count<2` → 多镜头连贯性不适用；无音轨 → `audio_sync` 不适用；等等。避免模型对不相关叶子误报后触发扣分或门控。

**`story_logic_coherence` 的 `dim="story"` 是有意的**：`"story"` 不在 `MODE_WEIGHTS`/`DIM_NAMES` 里，所以它的扣分**不进加权总分**——否则会串进别的维度、改变所有历史分数。它只服务标注口径与 `dim_badcase`。**不要当 bug 修掉。** 它的判定并入 `UNIFIED_SCAN_SYSTEM` 第七节（零额外调用）；回退到分维度路径时不补检测器，保持 `not_observable`。

`BADCASE_KEYS`（`rubric.py:201`）把六个自测维度映射到「主判据 → 辅助判据」叶子，供 `eval_f1.py` 做二分类 P/R/F1。

## 打分与拦截（`scoring.py`，全部确定性）

**加权维度只有 5 个**，两套权重，**没有 Omni、没有 integrity/audio 权重**：

| 模式 | consistency 主体一致性 | instruction 指令遵循 | collapse 动态崩坏 | visual 画面异常 | text 文字异常 |
|---|---:|---:|---:|---:|---:|
| T2V | 20% | 30% | 20% | 20% | 10% |
| I2V | 30% | 25% | 20% | 15% | 10% |

模式判定 `detect_mode`(`:526`)：**有图参考 → I2V，否则 T2V**。纯音频/纯参考视频不算 I2V。

- `apply_rubric_scores`：violated 叶子按 severity 扣分 `{0:0, 1:8, 2:22, 3:40}`，`hard=False` 的观感类（greasy / exposure_color）× `SOFT_LEAF_MULTIPLIER=0.6`。**rubric 只能压低、不能抬高**：`final = min(模型分, 100 - 扣分)`；模型没给某维分时按 `None` 处理，不当 0。
- **`visual` 与 `text` 两维纯由 rubric 驱动**（`model=None`），不采信模型自评，避免内容指令类问题串味。
- `apply_hard_gates`：collapse≥2 / 任何 sev≥3 / 硬约束 sev≥2 / 文件异常 sev≥2 / 模型自判 `blocked` → `status=blocked`（**不封顶 total**）。模型自报的 `blocked` 永不下调。
- `apply_veto`：**一票否决封顶 `total=20`** 且 blocked——仅两条：`collapse_level ≥ 2`、`visual_exposure_color` violated 且 sev ≥ 3。阈值常量在 `scoring.py:47-51`，扩展改 `VETO_*`。
- `weighted_total`：按 `MODE_WEIGHTS` 加权求和。

⚠️ **`scoring.py:8-11` 的模块顶部注释是过时的**（写的是 `dynamic/visual/prompt/audio` 四维），实际 dict 是上表五维。读代码别被它带偏；要改就连注释一起改。

## 标注口径适配层（`annotation.py`）

内部体系（16 叶子、5 加权维度、severity 0-3）与**人工标注规范**（8 个维度、三档得分）不是一套东西。`annotation.py` 只做**单向映射**，产出交付用的标注口径结论挂在 `report["annotation"]`，同时给每张 evidence 卡补 `annotation_dims / annotation_score / annotation_reasoning`（`static/studio.js:annotationOf` 优先用它们）。

- 8 个维度（`ANNOTATION_DIMS`）：指令遵循、参考一致性、视觉一致性、音频综合质量、画面质量、动态质量、文字质量、故事逻辑连贯性。
- 三档得分：`0.0 恶劣错误` / `0.5 瑕疵` / `1.0 无问题`（`TIER_BAD/BLEMISH/CLEAN` + `TIER_WORD`）。

三条不可破坏的性质：

1. **只读、不回流**：跑在所有打分/门控/veto **之后**，`score_total` 与 `final.status` 完全由 `scoring.py` 决定。已验证：对 306 份历史报告重跑打分链，结果与改动前逐字节一致。
2. **纯函数**：不 import `judge_pipeline`，只依赖 `rubric` + 标准库，所以能离线跑在任意历史报告上（改映射表后先用 `runtime/results/*.json` 全量回归，零模型成本）。**它刻意不参与 `CACHE_VERSION`。**
3. **不把「没判过」说成「没问题」**：叶子缺失或全 `not_observable` 的维度输出 `applicable=False` + `tier="未评测"`（`NA_NOT_JUDGED`），与规范意义上的「不适用」（`NA_NOT_APPLICABLE`，如无参考图）分开记。

两处刻意与内部 `dim` 不同的归属（适配层存在的理由）：`audio_sync` 内部 `dim="instruction"` 但标注口径归**音频综合质量**；`instruction_audio` 留在**指令遵循**（规范：prompt 指定的音频/文字没做到算指令遵循）。`consistency_temporal_drift` 与 `dyn_collapse_major` 按规范要求**双记**两个维度。

`evaluations[].dimension` 在 `judge_pipeline.py` 里已被压成四值，未压缩的归类信息只在 `prompt_graph.constraints` 里（`rule_id` → `id` 在 306 份缓存上 7262/7262 全可 join），所以 `route_evaluation` **回查 constraint** 的 `category`/`dimension`，不改那个 remap。

## 关键文件

| 文件 | 作用 |
|---|---|
| `app.py` | HTTP 服务：手写路由、异步作业 + 轮询、静态与媒体分发（**Range/206 必须支持**，否则报告页视频拖不动）、人评/盲测/统计/listwise 端点 |
| `judge_pipeline.py` | 主编排（4980 行）：`analyze_case`、`GeminiObserver`/`GeminiZoomInspector`/`GeminiVerifier`/`GeminiGroundingLocator`/`GeminiTargetedRechecker`、案例发现、`work_dir`、`build_cost`、缓存 |
| `agent_judge.py` | **另一套并列编排**：给模型工具（抽帧/裁剪/成对比对/委派 Keye）自主取证的 agentic 判定。`analyze_case_agentic`。刻意不复用上面的角色类 |
| `rubric.py` | RUBRIC 16 叶子、`coerce_severity`、`normalize_rubric`、`rubric_hard_failures`、`disagreements`、`rubric_checklist`、`BADCASE_KEYS` |
| `rubric_prompts.py` | 各阶段 system prompt（COMPILER / OBSERVER / ZOOM / UNIFIED_SCAN / VERIFIER / COLLAPSE_VOTE / COLLAPSE_PAIR / AUDIO_RECHECK / GROUNDING …） |
| `scoring.py` | `MODE_WEIGHTS`、`apply_rubric_scores`、`apply_hard_gates`、`apply_veto`、`weighted_total`、`dimension_summary` |
| `annotation.py` | 标注口径适配层（8 维 + 三档 + 结构化反馈）：`annotate_report`、`route_evaluation`、`LEAF_TO_DIMS` |
| `integrity.py` | ffmpeg 封装：`inspect_video`、`check_integrity`、`motion_profile`、`sample_frames_motion_aware`、`extract_native_frames`、`extract_region_crops`、镜头切分 |
| `evidence.py` | 每条「不通过」配一张原生帧证据：`build_evidence`、`annotate_frame_bbox` |
| `gemini_client.py` | stdlib generateContent：Vertex SA 自签 JWT (`_vertex_bearer`)、429/5xx 指数退避、JSON 修复级联、`generate_with_tools`、`evict_stale_media` |
| `openai_client.py` | OpenAI 兼容（本地 Keye / vLLM），含上下文溢出降级（`_shrink_video` / `_halve_images`） |
| `config.py` | `load_gemini_config` / `load_openai_config`：env 优先，回退解析 `../gemini.md` |
| `trace_store.py` | `TraceSession`：调用轨迹全量落盘 `runtime/traces/<run_id>.json` |
| `report_html.py` | 自包含 HTML 报告（图片 base64 内嵌） |
| `listwise.py` / `listwise_html.py` | 多候选横向对比 + 其报告页 |
| `upload_case.py` | 手写流式 multipart 解析、`normalize_prompt`、`content_slug`（内容寻址）、`finalize_staging` |
| `dataset_cache.py` | 离线数据集 `../mm-datasets/*/cache` 只读浏览 + 缩略图 |
| `static/studio*` | 唯一页面：上传 / 数据集选材 + 标注播放器（本地案例集、盲测、统计页已删） |

**脚本**：`build_dataset_cache.py`（xlsx → 数据集 cache）、`batch_dataset.py`（按磁盘 cache 并发批跑，断点续跑）、`batch_0807.py`（0807 集下载+批跑）、`eval_f1.py`（五个自测集 P/R/F1，需 pandas）、`smoke_scan.py`（单 case 冒烟，断言 5 个扫描字段都在）。

**仓库外硬编码依赖**（`Path(__file__).parents[1]`）：`../I2VA_gold50_AI`、`../T2VA_gold50_AI`、`../mm-datasets/0811-dataset/cache`、`../gemini.md`。注意 `COLLECTION_PREFIXES = ("I2VA", "T2VA")`（`judge_pipeline.py:97`）——磁盘上若有 `V2VA_*` 集合**永远扫不到**。

## 产物目录（`runtime/`，已 gitignore）

```
runtime/results/{slug}__{source}__{engine}__{mode}__{CACHE_VERSION}.json   完整报告缓存
runtime/traces/<run_id>.json                                              调用轨迹
runtime/reports/<run_id>.html                                             自包含报告
runtime/work/<slug>__<source>/[<run_id>/]<name>/                           抽帧/裁图中间产物
```

`runtime/` 下若还留着 `self_scores.jsonl` / `arena_votes.jsonl` / `arena_orient.json`，是人评与盲测页面存在时期的遗留数据，已无任何代码读写。

`work_dir` 带 `run_id` 分层是**必须的**：`extract_native_frames` 会清空目标目录的 `zoom_*.jpg`，同一条视频的并发 run 共用目录时，后启动的会删掉先启动的帧，导致 `scan_unified` 抽不到帧、**静默回退到 37 次调用的旧路径**。

## 环境开关

配置来源分三处，前缀不统一，没有集中清单：`config.py`（`GEMINI_*` / `OPENAI_*`）、`judge_pipeline.py:100-260`（40+ 个 `JUDGE_*`）、`agent_judge.py:45-60`（`JUDGE_AGENT_*`）。常用的：

| 变量 | 默认 | 说明 |
|---|---|---|
| `VIDEO_JUDGE_PORT` | 9900 | 服务端口 |
| `GEMINI_SEND_WHOLE_VIDEO` | 1 | 整段视频原生输入 |
| `GEMINI_VIDEO_MAX_MB` | **512** | 超限改发抽帧（老文档写 30，早就改了） |
| `GEMINI_MODEL` | `gemini-3.1-pro-preview` | |
| `JUDGE_UNIFIED_SCAN` | 1 | 4b 合并扫描；置 0 回退 37 次调用的分维度路径 |
| `JUDGE_UNIFIED_SCAN_FRAMES` / `_MAX_SIDE` / `_VIDEO` | 16 / 1280 / 1 | 合并扫描预算 |
| `JUDGE_UNIFIED_FALLBACK` | 1 | 合并扫描失败是否回退。**批量跑建议置 0**：宁可这条报错重试，也不要静默烧 5 倍的钱 |
| `JUDGE_TARGETED_RECHECK` / `_MAX_FRAMES` | 1 / 4 | 条件触发高清复核 |
| `JUDGE_SKIP_EVIDENCE` | 0 | 跳过证据帧标注（省成本） |
| `JUDGE_GROUNDING_PASS` / `_MAX_FRAMES` | 1 / 8 | 问题定位（补 bbox） |
| `JUDGE_{CONSISTENCY,INSTRUCTION,COLLAPSE,VISUAL,TEXT}_SCAN` | 1 | 分维度专项检测器各自开关（仅回退路径生效） |
| `JUDGE_COLLAPSE_PAIR_MIN_HITS` | 1 | 崩坏成对比对判定门槛。1=偏召回（6 条样本实测 P.67/R.67/F1 .67）；2=偏精确（P1.0/R.33/F1 .50） |
| `JUDGE_SCAN_CHUNK_WORKERS` | 4 | 单 case 内分块并发数 |

## 成本

Gemini 计费：输入 ¥11.97/百万 token、输出 ¥71.79/百万（思考 token 计入输出价，`judge_pipeline.py:3630`）。`build_cost` 产出 `cost.price`，报告页显示每 case 预估成本。走合并扫描时单 case 约 8–9 次调用；视频虽大但只占 ~300 token（模型高度压缩表征），传整段视频不比传关键帧贵。**回退到分维度路径会变成约 37 次调用、~5 倍成本**——批量跑前确认 `JUDGE_UNIFIED_SCAN=1`。

## 约定与坑

- **不主动加注释**；但现存的中文长注释是**决策记录**（写了「为什么这个阈值」「哪条路实测更差」），不要顺手清理。遵循现有风格：stdlib、类型注解、中文文案。
- 模型返回 JSON 可能含非 dict 脏元素——各处已用 `isinstance(x, dict)` 过滤，新增消费点也要防御。severity 也可能是 `"严重"`/`"2"`/`None`，统一走 `rubric.coerce_severity`，不要裸 `int()`（实测 30 条里挂过 2 条）。
- `extract_native_frames` 每次清空目标目录的 `zoom_*.jpg`——多轮抽帧务必用不同子目录（用 `work_dir(..., run_id=...)`）。
- 音频只能在观察器阶段判（后续步骤拿不到音频；4c 独立复听已停用）。Gemini 音频感知偏弱是已知局限。
- 拆解阶段 `dimension` 是开放枚举，rubric 是固定 16 叶清单，两套体系（抽取 vs 打分），语义重叠属正常。
- `upload_case.normalize_prompt` 必须把 CRLF 归一到 LF，否则 `content_slug` 算不一致、浏览器提交打不中批处理缓存。
- `dataset_cache.py:50 _LABEL_RE` 与 `static/studio.js` 的 `LABEL_RE` **必须保持同构**，靠注释约束、无测试保障；一旦漂移会静默把参考图对错主体。
- 前后端契约靠约定维持：report JSON 的约 40 个顶层字段被 `studio.js`/`report/html.py`/`judge/listwise.py`/`batch/eval_f1.py` 四处各自按 key 取用，改字段名要同步四处。
- 大量 `except Exception:  # noqa: BLE001` 是刻意的 best-effort（grounding / evidence / report / annotation / cache），单点失败不打断整条 case；代价是失败只出现在 UI 的 stage 文本里、不进日志聚合。
- **仓库无任何测试**（无 `test_*.py`、无 pytest 配置）。改动验证顺序：
  1. `python3 -c "import rubric,scoring,annotation,rubric_prompts,integrity,report_html,judge_pipeline"`
  2. `node --check static/studio.js`（连 `static/studio/*.js`、`static/shared/*.js` 一起过）
  3. mock 端到端：`analyze_case(..., use_vlm=False, allow_fallback=True)`
  4. 真实单 case：`JUDGE_SKIP_EVIDENCE=1 python3 smoke_scan.py`
  5. 动了打分/映射链 → 用 `runtime/results/*.json` 全量离线回归（零模型成本）
- `secrets/vertex-sa.json` 是 service account 私钥，已 gitignore 但明文在工作区，别 cat 到日志或提交。
