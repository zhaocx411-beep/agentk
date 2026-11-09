# Workflow + Agent 系统逻辑

## 核心架构

整体采用**混合架构：固定工作流骨架 + 特定阶段的有界 Agent 循环**，主入口为 `judge_pipeline.py:917` 的 `analyze_case()`。

设计哲学可以概括为一句话：**判断委托给 LLM，但拦截不完全信任 LLM。** 模型负责"看到了什么、规则是否违反"，程序则牢牢把控"确定性检查、评分、加权、强制拦截"。有一条硬规则贯穿全局：**高画面质量分数不能覆盖关键失败**（如崩坏、结构违规等）。

---

## 7 步工作流总览

| 步骤 | 阶段名 | 执行方式 | 输入 | 输出 | 说明 |
|------|--------|---------|------|------|------|
| **①** | 视频文件检查 | Tool Call (ffprobe/ffmpeg) | 视频文件 | metadata + integrity | `blackdetect` + `freezedetect` 做确定性检查，integrity 已降级为预检门，不再作为加权维度 |
| **②** | 运动感知抽帧 | Tool Call (ffmpeg) | 视频 + metadata | 关键帧 + 运动窗口 + contact sheet | 四个阶段：逐帧运动量计算 → 高运动窗口检测 → 采样规划 → 单次提取。任一环节失败则回退均匀采样 |
| **③** | 提示词约束提取 | **VLM**（文本 + 参考图） | prompt-text + prompt-image | 可验证约束清单 | 模型输出的是 verifiable constraints（可验证约束），不是简单复述；程序会强制注入 4 条基线约束 |
| **④** | 视频观察理解 | **VLM**（视频输入） | 整段视频（含音轨） + 参考图 | observations + preliminary_issues + focus_intervals | 全流程中唯一"看视频"的步骤，同时初步判定音画同步。输出三部分：可见事实、初步判定、需放大时刻。视频过大时回退为帧图模式 |
| **④b** | HD 放大检查 + 专科扫描 | **VLM**（原始分辨率图像） | focus_intervals 处的原生帧 | zoom_findings + 3 项专科扫描结论 | **有界 Agent 循环**：模型可主动请求更多帧（≤2 轮 / ≤4 帧）。4 个专科扫描各自独立运行：HD 放大、崩坏扫描（自适应投票 1-3 票）、画面异常扫描、文字可读性扫描 |
| **④c** | 音频复听（条件触发） | **VLM**（带音频视频） | 整段视频（含音轨） | av_sync 修正结论 | 仅当 observer 报告 av_sync 置信度低时才触发 |
| **⑤** | 逐条裁决核对 | **VLM**（纯文本） | 约束清单 + observations + zoom 结论 + integrity | evaluations + scores + rubric + status | **无视频、无图片**，纯文本推理。`not_observable` 不等于 `violated`。HD 结果优先于视频观察结论 |
| **⑥** | 汇总评分 + 硬门 + 否决 | Tool Call（确定性程序） | verifier 输出 + 专科扫描 | 最终分数 + status + hard_failures | 专科扫描**权威覆盖**对应 rubric leaf → rubric 扣分（只能降不能升）→ 硬门（严重违规直接 blocked）→ 否决（崩坏 ≥2 或严重色彩异常 → 总分封顶 20） |
| **⑦** | 证据标注 + 报告 | Tool Call（确定性程序） | 所有 issue + 时间戳 | 带红框证据图 + 自包含 HTML 报告 | 每个失败项提取原生帧，有 bbox 的画红框标注 |

---

## 各步骤详细说明

### 步骤①：视频文件完整性检查

- **文件**: `integrity.py` -- `inspect_video()` (line 25)、`check_integrity()` (line 62)
- **调用位置**: `judge_pipeline.py` lines ~991-1005
- **工具**: `ffprobe` 读取元数据（分辨率、帧率、时长、编码、帧数、文件大小、音轨）；`ffmpeg` 跑两个确定性滤镜：
  - `blackdetect=d=0.2:pix_th=0.10` -- 检测持续黑帧
  - `freezedetect=n=0.003:d=0.5` -- 检测冻结/卡帧
- **校验逻辑**: frame_count ≈ duration × fps，偏差超过 15% 即判定异常
- **输出**: 0-100 的 integrity 分数 + 问题列表
- **定位**: 作为 sanity 预检门存在，不再是加权维度。severity ≥ 2 的 integrity 问题会进入硬门

### 步骤②：运动感知关键帧提取

- **文件**: `integrity.py` -- `motion_profile()` (line 137)、`plan_sample_times()` (line 196)、`sample_frames_motion_aware()` (line 232)
- **调用位置**: `judge_pipeline.py` lines ~1007-1016，通过 `prepare_frame_bundle()` (line ~410) 调用
- **四个阶段**（全程仅用 ffmpeg，不依赖 numpy/opencv）：
  1. **逐帧运动量计算**: `ffmpeg -vf "fps=8,scale=160:-1,tblend=all_mode=difference,signalstats,metadata=print:key=lavfi.signalstats.YAVG"`，输出 `[{time, motion}]` 序列
  2. **高运动窗口检测**: 阈值取 `mu + 0.8×sigma`，相邻超阈值样本合并为一个运动窗口
  3. **采样规划**: 先铺一层基础均匀网格（保证全覆盖），再在高运动时刻密集补帧。最小间隔 0.12s，上限 12 帧
  4. **单次提取**: `ffmpeg -vf "select='eq(n\,12)+eq(n\,36)',scale=512:-1" -vsync 0`，每帧标记时间戳和 dense 标志
- **回退策略**: 任何环节出错，回退到均匀采样（`extract_keyframes()` at line 92）
- **额外产物**: 生成 6fps 的 contact sheet（6×5 tile），通过 `build_contact_sheet()` (line ~388) 生成
- **关键发现**: 图像 token 与分辨率无关 -- 512px 的帧和原生 1200×1724 的帧都约消耗 ~1,085 tokens，所以抽帧直接用全分辨率，最大化细节呈现

### 步骤③：提示词约束提取（VLM 调用 #1）

- **文件**: `judge_pipeline.py` -- `compile_prompt_vlm()` (line ~560)、`compile_prompt_heuristic()` (line ~497)
- **System prompt**: `rubric_prompts.py` 中的 `PROMPT_COMPILER_SYSTEM` (line 1)
- **输入**: 完整 prompt 文本（中/英）+ 最多 4 张参考图（base64 `inline_data`）
- **模型输出**: 可验证约束清单 -- 注意这不是对 prompt 的复述，而是拆解出可逐条验证的约束。每条约束包含 id、category、statement、source（prompt 原文摘录）、hardness（hard/soft）、dimension（prompt/dynamic/visual/audio）
- **程序强制注入**: 4 条基线质量约束（DYN-COLLAPSE、DYN-PHYSICS、VIS-RENDER、CON-TEMPORAL），哪怕模型漏掉了也会补上
- **音频约束**: prompt 中出现的台词/对话/音效/音乐，归入 dimension=audio
- **输出**: `prompt_graph` dict，包含 entities、events、constraints、physics_exceptions、text_requirements、reference_expectations
- **回退**: `compile_prompt_heuristic` 是离线/mock 回退方案，不含任何硬编码案例关键词

### 步骤④：视频观察理解（VLM 调用 #2）

- **文件**: `judge_pipeline.py` -- `GeminiObserver.observe()` (line ~640)
- **System prompt**: `rubric_prompts.py` 中的 `OBSERVER_SYSTEM` (line 44)
- **视频输入模式决策**:
  - `SEND_WHOLE_VIDEO=1`（默认开启）且视频 ≤ `VIDEO_MAX_MB`（默认 30MB）: 直接发**整段视频** base64 `inline_data`（mime_type: video/mp4），走原生视频理解
  - 视频过大: 回退为发送 8 张关键帧 + contact sheet（降级模式）
- **附加输入**: 参考图（≤4 张）、高运动窗口提示、分辨率校准说明（告知模型源分辨率，以便区分低分辨率软化和真实缺陷）
- **音频**: 整段视频发送时**包含音轨**，observer 会在这一步同时初步判定音画同步（av_sync），音频仅消耗约 125 tokens。步骤④c 是在此基础上的条件复检，仅当此处 av_sync 置信度低时才触发
- **模型输出三部分**:
  1. **observations** -- 仅记录可见事实（物体、位置、朝向、接触、轨迹、文字、光照），不确定的标 `not_observable`，不做任何结论性判断
  2. **preliminary_issues** -- 对每个 rubric 维度给出初步判定（pass/violated/not_observable/not_applicable）+ severity + evidence + bbox
  3. **focus_intervals** -- "可能有问题但视频分辨率看不清楚"的时刻，需要后续 HD 放大确认，最多 4 个
- **额外输出**: `collapse`（0-3）、`visual`（render_artifacts、text_status）、`audio`（av_sync、av_sync_confidence）、`identity`
- **关键设计**: observer 是全流程唯一"看视频"的步骤，也会给出初步判定，但这不是最终结论。verifier（步骤⑤）会基于事实独立复审。两者一致 = 高置信，不一致 = 标记人工复审
- **成本**: 视频仅消耗 ~300 tokens（9+ MB 文件），音频约 ~125 tokens

### 步骤④b：HD 放大检查 + 专科扫描（VLM 调用 #3-#6）

仅当处于原生视频模式（非 fallback）且未设置 `SKIP_ZOOM` 时才运行。

#### ④b-i: HD 放大复查（有界 Agent 循环）

- **文件**: `judge_pipeline.py` -- `GeminiZoomInspector.inspect()` (line ~690)
- **System prompt**: `rubric_prompts.py` 中的 `ZOOM_SYSTEM` (line 130)
- **流程**:
  - 取步骤④的 `focus_intervals`（若为空则回退到最高运动窗口峰值）
  - 用 `extract_native_frames()`（`integrity.py` line 113）以 `-q:v 2` 高质量提取**原始分辨率**帧
  - 连同先前怀疑一起发给模型
  - **Agent 循环**: 模型可以回复 `next_requests`（要求看更多时间戳）或 `done=true`（确认完毕）。循环上限为 `ZOOM_MAX_ROUNDS=2` 轮 / `ZOOM_MAX_FRAMES=4` 帧
  - 每轮使用独立子目录（`..._zoom/round{r}`），避免 `extract_native_frames` 清空目录时互相干扰
- **输出**: `zoom_findings`（确认或推翻的怀疑，含 bbox、severity、confidence）、frames、rounds

#### ④b-ii: 崩坏专科扫描 + 自适应投票

- **文件**: `judge_pipeline.py` -- `GeminiZoomInspector.scan_collapse()` (line ~760)、`vote_collapse()` (line ~840)
- **System prompt**: `rubric_prompts.py` 中的 `COLLAPSE_VOTE_SYSTEM` (line 180)
- **流程**:
  - **独立于** observer 的 focus_intervals -- 对全片做运动感知采样（≤`COLLAPSE_SCAN_FRAMES=14` 原生帧），确保高召回率，不漏掉真实崩坏瞬间
  - 发送给模型做崩坏判定
  - **自适应投票**: 先投 1 票。如果结果明确自信（clean 且无帧被标记，或 collapse 且 severity≥3）→ 直接停止；如果模糊（不确定，或 collapse 但只 sev2，或 clean 但有帧被标记）→ 继续，总共投 `COLLAPSE_VOTES=3` 票取多数
  - 多数判定为崩坏时，severity 取崩坏票的中位数，最低为 2
- **输出**: verdict（collapse/clean）、severity、score（1-5）、per-frame findings

##### 相邻帧对比的误报过滤（`_filter_pair_finding`）

相邻帧对比是崩坏维度的主要召回来源，但它同时是唯一的误报来源。全量 158 条里
collapse 的 6 个误报全部来自这里，且集中在**本来就不保持固定结构的东西**上：

| case | 模型的"崩坏"证据 | 实际是什么 |
|---|---|---|
| `broken_029` | 玻璃杯中冰块数量减少、位置异常变化 | 可流动细碎物 |
| `broken_032` | 奶茶里珍珠数量分布从密集变稀疏 | 同上 |
| `broken_030` | 巧克力球表面颗粒模糊、包装纸褶皱错位 | 织物/颗粒 |
| `broken_031` | T恤正面文字排版位置改变 | 衣服印字（属文字维度） |

规则本身写在 `COLLAPSE_PAIR_SYSTEM` 里，但**模型不照做**，所以在代码里再兜一层：
被标记 abnormal 的对象通篇命中非结构词表（`_NONSTRUCT_OBJECT`）且**没有提到任何
身体部位**（`_STRUCT_OBJECT`）时，把 `collapse` 降级为 false。

判据刻意保守。曾试过两条更狠的闸——「说不出具体结构缺陷就降级」和「理由只落在
模糊上就降级」——它们能多修掉 T恤图案那类误报（模型会把它挂在"上半身"这个结构性
对象名下逃过闸②），但同时把 3 条靠"头发模糊/裙摆/切镜"蒙对的正例打成漏报。
34 条全量回放：

| 配置 | TP | FP | FN | TN | P | R | F1 |
|---|---|---|---|---|---|---|---|
| 不过滤 | 18 | 6 | 5 | 5 | 0.750 | 0.783 | 0.766 |
| 只保留闸② | 16 | 4 | 7 | 7 | **0.800** | 0.696 | 0.744 |
| 三道闸全开 | 15 | 3 | 8 | 8 | 0.833 | 0.652 | 0.732 |

取「只保留闸②」：优先降误报，用可解释的对象类别做判据，不牺牲太多召回。

#### ④b-iii: 画面异常专科扫描

- **文件**: `judge_pipeline.py` -- `GeminiZoomInspector.scan_visual()` (line ~825)
- **System prompt**: `rubric_prompts.py` 中的 `VISUAL_SCAN_SYSTEM` (line 290)
- **流程**: 提取 ≤`VISUAL_SCAN_FRAMES=9` 帧，分别判定曝光/色彩异常（exposure_color）和油腻/塑料/AIGC 感（greasy）
- **重点**: 以面部油腻/蜡感/塑料感作为主要指标，曝光/色彩问题作为次要指标
- **输出**: per-frame findings、exposure_color verdict+severity、greasy verdict+severity、score（1-5）

##### 帧级证据优先于整体结论（`_lift_by_frames`）

全量 158 条里 visual 的 10 条漏报几乎都是同一个句式：

> 画面整体质感自然，**仅个别帧有轻微质感问题，不影响整体观感**

模型在 `per_frame` 里已经老实标出了 `greasy_severity`，却在块级 verdict 上用整体印象把它抹平。
标注口径是「出现即算」，所以 `_lift_by_frames()` 按 `max(per_frame severity)` 反推子项结论：
帧级达到 severity≥2 就把 verdict 抬成 violated，不让整体印象覆盖已列出的局部证据。
这是纯确定性聚合，不额外花任何 token。

#### ④b-iv: 文字可读性专科扫描

- **文件**: `judge_pipeline.py` -- `GeminiZoomInspector.scan_text()` (line ~865)
- **System prompt**: `rubric_prompts.py` 中的 `TEXT_SCAN_SYSTEM` (line 360)
- **流程**: 提取 ≤`TEXT_SCAN_FRAMES=12` 张原生分辨率帧，判定视频中**任何显著生成文字**（字幕、招牌、UI、海报、包装等）是否不可读/乱码/字符损坏
- **输出**: per-frame findings、verdict（violated/pass/not_applicable）、severity、score（1-5）

##### 文字维度的三条并行通道

单靠「整帧看一眼判可读性」两头都不够，所以拆成三条互补通道：

| 通道 | 方法 | 解决什么 |
|---|---|---|
| ① 分块整帧 | 12 帧按 3 帧/块，逐帧先抄写再判定 | 大字幕、明显招牌 |
| ② 定位→裁剪放大 | 视频通道定位 `time+bbox` → `extract_region_crops` 裁出并放大到 768px → 复判 | 小字（加湿器侧面品牌名、车厢编号）：整帧下只有几十像素，无论整帧分辨率给多高都会被 Keye 的单帧 token 上限抹平 |
| ③ 转写合法性（纯文本） | 收集 ①② 抄下来的所有字符串 → 只把**字符串**交给模型判「是不是真实词」 | 模型「抄得出乱码却判可读」 |

通道③是本轮最大的召回来源。全量 158 条里 text 维度 13 条漏报，绝大多数长成同一个样子：

> 三帧画面中仅在 t=1.58s 出现清晰可见的文字 **"RIAA BAEY"**，
> 该文字**字形完整、拼写正确，无不可读、乱码或字形崩坏现象**

也就是 **OCR 转写能力是够的，视觉可读性判定能力不够**。于是把判定从模型的弱项
（看像素判字形坏没坏）挪到它的强项（看字符串判是不是词）。纯文本调用，几百 token，
比再加一轮高分辨率视觉复判便宜得多。

实测 `RIAA BAEY` / `LIE LO` / `NFRUAE` 全部命中，`WIDE LEG` / `N E S W` / `SINHE` /
中文地名均不误报。

**通道③的关键约束（不加会反向制造误报）**：转写本身可能错，而「转写错」≠「画面文字坏」。
所以只有**字符串自身不成词**才算，并硬性豁免两类：

- **只差 1~2 个字母**（`Avuda`→`Ayuda`、`FRANCOIS OILS`→`FRANCE OILS`）→ 更可能是模型自己看错了
- **片段/截断**（`z cumpleaños`、`ppy hday`）→ 转写没抄全，或文字在动画中逐渐出现

这四个串正是不加豁免时产生的 3 个误报的来源。

##### 「模糊」不是「字形崩坏」——用代码强制

提示词里写了这条豁免，但模型不照做：实测把广告牌上的 `FRANCOIS OILS`、`TURKISH AIRLINES`
**都正确抄了下来**，理由却写「模糊不清、无法辨认具体字符」。`scan_text_regions()` 对
「理由只提模糊类词、不提具体字形缺陷」的 violated 做程序化降级
（`_BLUR_REASON` / `_GLYPH_REASON`）。模型会同时提两类词来对冲，这时不降级 —— 属已知残余误报。

### 步骤④c：音频复听（条件触发，VLM 调用 #7）

- **文件**: `judge_pipeline.py` lines ~1165-1190
- **System prompt**: `rubric_prompts.py` 中的 `AUDIO_RECHECK_SYSTEM` (line 260)
- **触发条件**: observer 报告了 `audio_sync` 且 `av_sync_confidence=low`，或者 av_sync 为 `minor_drift`/`out_of_sync` 且置信度非高；同时视频需要有音轨、处于整段视频模式、在大小限制内
- **流程**: 重新发送整段视频（含音轨），模型仅聚焦音画同步（唇同步）做复检
- **输出**: av_sync、severity、confidence、evidence

### 步骤⑤：逐条裁决核对（VLM 调用 #8）

- **文件**: `judge_pipeline.py` -- `GeminiVerifier.verify()` (line ~920)
- **System prompt**: `rubric_prompts.py` 中的 `VERIFIER_SYSTEM` (line 305)
- **输入**: 约束清单（步骤③）+ observations（步骤④）+ observer 初步判定 + zoom 结论（步骤④b）+ integrity 预检 + audio note
- **纯文本推理 -- 不发送任何视频或图片**
- **模型输出**: 对每条约束给出 verdict（pass/violated/not_observable/not_applicable）+ severity（0-3）+ quality_score + time + evidence + confidence + dimension；对 8 个固定 rubric leaf 给出判定；`scores`（各维度 0-100）；`hard_failures`；`collapse_level`；`rubric`；`status`（pass/needs_review/blocked）；`summary`
- **关键原则**:
  - `not_observable ≠ violated` -- 看不清不等于违规，防止因模糊/遮挡导致误判
  - HD 放大结果与视频观察冲突时，**HD 结果优先**（因为更清晰）
  - verifier 可以不同意 observer 的初步判定，分歧会被标记出来

### 步骤⑥：汇总评分、硬门、否决（确定性程序）

这一步是系统的确定性兜底，分多层处理：

#### 6a: Rubric 归一化 + 专科扫描覆盖

- **文件**: `rubric.py` -- `normalize_rubric()` (line 90)
- 将模型的 rubric 判定合并到固定的 8 叶 rubric 上
- 交叉校验 observer 初步判定 vs verifier 判定（一致/分歧/不适用）
- 从 `dyn_collapse_major` leaf 校准 collapse_level
- **专科扫描权威覆盖**（`judge_pipeline.py` lines ~1255-1300）-- 专科扫描结果作为权威信号，直接覆盖对应 rubric leaf：
  - 崩坏扫描: verdict=collapse → `dyn_collapse_major`=violated，severity 取扫描 severity；verdict=clean → `dyn_collapse_major`=pass（覆盖 verifier 的假阳性）
  - 画面扫描: exposure_color/greasy leaf 被覆盖（violated 且 sev≥2 时生效，否则 pass）
  - 文字扫描: visual_text leaf 被覆盖

#### 6b: Rubric 扣分

- **文件**: `scoring.py` -- `apply_rubric_scores()` (line 72)
- 每个违规 leaf 按 severity 扣分: severity 1 扣 8 分，severity 2 扣 22 分，severity 3 扣 40 分
- 软 leaf（greasy、exposure_color，`hard=False`）扣分乘以 0.6 系数
- **Rubric 只能降不能升**: `final = min(model_score, 100 - total_penalty)`
- **Visual 维度纯 rubric 驱动** -- 模型自由 form 的 visual 分数不采用，避免指令跟随问题污染画面质量评分
- 模型遗漏某维度分数时不归零

#### 6c: 硬门

- **文件**: `scoring.py` -- `apply_hard_gates()` (line 150)
- 任何违规 evaluation severity ≥3 → 硬失败
- 任何违规 HARD constraint severity ≥2 → 硬失败
- collapse_level ≥ 2 → 硬失败
- Integrity 问题 severity ≥2 → 硬失败
- 模型自身判定的 `blocked` 状态**永不下调**
- 最终结果: status = `blocked` / `needs_review` / `pass`

#### 6d: 一票否决（分数封顶）

- **文件**: `scoring.py` -- `apply_veto()` (line 32)
- `collapse_level ≥ 2`（明显崩坏）→ 总分封顶 `VETO_SCORE_CAP = 20`
- `visual_exposure_color` violated 且 severity ≥ 3（严重色彩/曝光）→ 总分封顶 20
- Status 强制改为 `blocked`
- **这就是那条核心规则的落地: 高画面质量不能补偿关键失败**

#### 6e: 加权总分

- **文件**: `scoring.py` -- `weighted_total()` (line 136)
- 权重 `MODE_WEIGHTS`:

| 模式 | Dynamic | Visual | Prompt | Audio |
|------|---------|--------|--------|-------|
| T2V  | 35%     | 30%    | 20%    | 15%   |
| I2V  | 32%     | 28%    | 20%    | 20%   |

- 模式自动检测: 有参考图 → I2V，无参考图 → T2V
- Integrity 和 consistency 已不再是加权维度

### 步骤⑦：证据标注 + 报告生成

- **文件**: `evidence.py` -- `build_evidence()` (line 115)、`report_html.py` -- `write_report_html()`
- **证据**: 为每个失败项提取原生分辨率帧，有 bbox 的用 ffmpeg `drawbox` 画红框标注，生成证据卡（含 scope、ref_id、title、verdict、severity、time、frame、has_box、bbox、note、source_stage、confirmed 等字段）
- **报告**: 生成自包含 HTML 文件（图片以 base64 内嵌，无需服务器即可打开），包含结论、维度分数表、三级 rubric 明细、关键帧、约束清单、逐条判定、证据图集、成本统计、完整的步骤级模型 I/O trace

---

## 固定 15 叶 Rubric

**文件**: `rubric.py`

| Key | L1 | L2 | L3 | 维度 | Hard |
|-----|-----|-----|-----|------|------|
| `consistency_input_reference` | 一致性 | 输入图一致性 | 首帧与输入图主体 ID/属性/色彩/画风一致 | consistency | Yes |
| `consistency_multishot_continuity` | 一致性 | 多镜头间一致连贯性 | 相邻镜头主体/道具/场景/情节连续 | consistency | Yes |
| `consistency_temporal_drift` | 一致性 | 动态时序一致性 | **同一镜头内**主体/环境/数量无凭空改变 | consistency | Yes |
| `instruction_basic_content` | 指令遵循 | 基础内容指令遵循 | 主体/动作/对象/风格/环境按 prompt 响应 | instruction | Yes |
| `instruction_multishot` | 指令遵循 | 多镜头指令遵循 | 镜头数量/顺序/切镜时点/镜头内信息 | instruction | Yes |
| `instruction_camera` | 指令遵循 | 运镜文本响应 | **镜头运动方式/方向/速度**按 prompt 响应 | instruction | Yes |
| `instruction_audio` | 指令遵循 | 音频指令遵循 | 台词/音效/音乐/时机按 prompt 响应 | instruction | Yes |
| `visual_exposure_color` | 画面质量 | 画面基础内容 | 曝光光影或色彩异常 | visual | No（soft，0.6x） |
| `visual_greasy` | 画面质量 | 画面基础内容 | 油腻感 | visual | No（soft，0.6x） |
| `visual_text` | 画面质量 | 画面基础内容 | 文字不可读 | **text** | Yes |
| `dyn_physics` | 动态质量 | 动态基础质量 | 不符合物理规律 | collapse | Yes |
| `dyn_collapse_major` | 动态质量 | 动态基础质量 | 明显崩坏 | collapse | Yes |
| `audio_sync` | 音频评测 | 音画同步 | 音画同步 | instruction | Yes |
| `prompt_spatiotemporal` | 提示词一致性 | 时空关系与动作目标 | 相对位置/朝向/动作-目标/时序 | instruction | Yes |
| `prompt_environment` | 提示词一致性 | 环境与场景响应 | 环境信息是否按 prompt 呈现 | instruction | No |

其中 `prompt_spatiotemporal` 的 `desc` 字段会强制模型按特定顺序推理：(1) 通过肩线/躯干/脸/脚确定身体朝向，(2) 确定其他主体的世界坐标位置/轨迹，(3) 区分屏幕坐标与世界坐标（尤其摄像机平移时），(4) 检查动作-目标绑定。这样做的目的是防止模型仅凭单帧的屏幕左右位置就下结论。

`consistency_temporal_drift` 与 `instruction_camera` 是为对齐 QA 标注口径而补的两个叶子：
标注里大量出现「单镜头内书架变成窗户」「四人变五人」（动态时序一致性）和
「Pedestal Down 未体现」「缓缓拉远缺失」（运镜文本响应），原先 13 叶都没有承载位置。

### 五个 P0 维度 ↔ 叶子映射

`rubric.py` 的 `BADCASE_KEYS` 把 15 个叶子归到 5 个自测集维度上，
既用于专项检测器的权威覆盖，也用于离线 P/R/F1 的二分类判定
（口径：对口叶子里出现 `violated` 且 `severity>=2` → 判该维度有 badcase）。

| 维度 | 自测集 | 对口叶子 |
|------|--------|---------|
| consistency | 主体时序一致性 | input_reference / temporal_drift / multishot_continuity |
| instruction | 核心指令遵循 | basic_content / camera / multishot / spatiotemporal / environment |
| collapse | 明显崩坏 | dyn_collapse_major |
| visual | 曝光颜色异常油腻感 | visual_exposure_color / visual_greasy |
| text | 文字不可读 | visual_text |

---

## VLM 调用汇总

模型: **本地部署的 `keye-vl`**（Kwai-Keye/Keye-VL-2.0-30B-A3B，经 `sglang_gateway`
在 `http://127.0.0.1:8000` 提供 OpenAI 兼容接口；`config.py` 的 `load_openai_config()`，
可用 `OPENAI_BASE_URL` / `OPENAI_MODEL` 覆盖）。`OpenAIClient.supports_video = False`
—— Keye 只接图片，所以观察器一律走「关键帧 + 全片缩略拼图」模式，不再直接送视频。

| 步骤 | 阶段 | 输入模态 | 调用数 | 目的 |
|------|------|---------|--------|------|
| ③ | prompt_compiler | 文本 + 参考图 | 1 | 从 prompt 提取可验证约束（含 `camera_requirements`） |
| ④ | observer | 关键帧（≤8）+ 拼图 + 参考图 | 1 | 记录事实、初步判定、标记需放大时刻 |
| ④b-i | targeted_recheck | 原生帧（≤4） | 0-1 | 对可定位疑点做一次高清复核 |
| ④b-ii | consistency_scan | 输入图 + 帧对/三元组 | 1 + 每镜头 1~2 + 每切镜边界 1 | **主体时序一致性**专项（成对比对） |
| ④b-ii' | consistency_regions | 视频（定位）+ 裁剪对 | 1 + ≤4 | 一致性**区域放大**通道（小目标漂移） |
| ④b-iii | instruction_scan | prompt 原文 + 帧（≤10） | 1 | **核心指令遵循**专项（强制先出 checklist） |
| ④b-iv | collapse_scan | 原生帧分块 + 相邻帧对 | ⌈12/3⌉ + ≤6 | **明显崩坏**专项（逐帧 + 相邻帧对比双通道） |
| ④b-v | visual_scan | 帧分块（4 张/块） | ⌈8/4⌉ | **曝光颜色/油腻感**专项 |
| ④b-vi | text_scan | 原生帧分块（3 张/块） | ⌈10/3⌉ | **文字可读性**专项 |
| ④b-vi' | text_regions + transcript | 视频（定位）+ 裁剪 + **纯文本** | 1 + ⌈n/2⌉ + 1 | 文字**区域放大**与**转写合法性**通道 |
| ⑤ | verifier | 纯文本 | 1 | 独立裁决每条约束 + 15 个 rubric leaf |

**典型成本**: 单 case 约 20-25 次调用、2-4 分钟（分块间并发，
`JUDGE_SCAN_CHUNK_WORKERS` 控制）。

---

## 为 Keye 做的三项关键适配

从 Gemini 迁到本地 Keye 时踩到三个坑，都不是"换个 endpoint"能解决的，
它们直接决定了 F1 能不能跑起来：

### 1. 图像 token 与分辨率成正比 → 必须做分辨率预算

Gemini 对每张图收固定 ~1080 token，与分辨率无关，所以老代码一律送**原生分辨率**
（`extract_native_frames` 的注释还写着"没有理由降采样"）。Keye 是 Qwen2-VL 式
动态分辨率 ViT，token ≈ `(H/28) × (W/28)`：

- 一张 1080p 帧 ≈ 2.6k token；
- **一张 4896×3264 的用户输入参考图 ≈ 20k token**，单张就撑爆 32k 上下文
  （实测 `HTTP 400 ... 29473 tokens from the input messages`）。

对策：
- `extract_native_frames(..., max_side=N)` 按 ffmpeg `scale` 限制长边，各专项检测器
  按「它需要多少细节」分别设预算（文字 1280 / 崩坏 1120 / 一致性 1120 / 油腻 896）；
- `encode_image_capped()` 把参考图统一压到长边 1024，并按 size+mtime 落盘缓存；
- `OpenAIClient` 把 `max_tokens` 从 8192 降到 4096，给输入腾空间；
- 兜底：`_halve_images()` 在命中上下文超限时**隔一张丢一张图**重试，
  而不是让整个 case 失败。

### 2. 多图一次送 → 注意力摊薄 → 感知维度全面漏报

原设计把 12 张帧塞进一次调用。Keye-30B-A3B（MoE，激活 3B）在这种输入下会退化成
**模板化输出**：对 12 张图给出 12 条一模一样的 note（"画面中人物服饰与姿态正常"×12），
把肉眼可见的主体熔合、眼部崩坏全部漏掉 —— 崩坏维度召回率 **0/3**。

对策：**分块扫描**（`_scan_chunks`）。把帧切成 2~4 张一组，每组单独一次调用、
组间并发，聚合口径为「任一块发现问题即算问题」。每张图拿到的注意力提升数倍，
单请求 token 也降下来了，可以顺带提高每帧分辨率；因为块间并发，单 case 墙钟时间基本不变。

### 3. Keye 判「单帧解剖结构是否正常」不准，但判「前后变化是否异常」很准

这是最关键的发现。对一张女性躯干已经熔进男性白袍、一只眼睛已经崩掉的动画帧，
即使只送这**一张**图、并用强制枚举的中立提问（"数一下手指、每只眼睛是否完整"），
Keye 依然回答 `eyes_ok: true`、`anatomy_ok: true`、`any_two_subjects_merged: false`。
**这是模型能力上限，不是 prompt 问题** —— 继续雕 prompt 或继续减少每次的图数都无效。

但把**相邻两帧**摆在一起、只问"逐个对象比一遍，什么变成了什么、这个变化正常吗"，
同一处异常立刻就被识别出来（`collapse=true, severity=2`，证据"女性角色凭空消失"）。

对策：崩坏检测的**主力通道改成相邻帧对比**（`COLLAPSE_PAIR_SYSTEM` +
`_collapse_pairs()`），逐帧绝对判断退为辅助通道。同理，一致性检测也从
「10 张帧 + 三个子项一起判」改成三组各自独立的**成对比对**任务
（输入图 vs 首帧 / 同镜头内三帧 / 切镜边界前后两帧），召回从 0/3 变为可用。

一句话总结这条经验：**把「绝对判断」任务改写成「对比判断」任务，是在能力受限的
本地模型上换取召回率最有效的手段。**

### 附：误报抑制

相邻帧对比换来召回的同时会把**运动模糊**误判成结构崩坏（奔跑腿部特写的负例即如此）。
因此加了两道确定性约束，而不是继续加 prompt 措辞：
- **互证要求**：认定崩坏需「至少一个窗口 severity=3」或「至少两个不同时间窗口都报明显崩坏」。
  真崩坏通常持续若干帧、会被多个窗口同时抓到；模糊误判往往只出现在孤立一处。
- **模糊自查**：prompt 里明确要求「若你的理由里出现『模糊/不清晰/看不清』，
  那就不是崩坏」，把模型自己的措辞变成判别信号。

### 附：severity 容错

模型会把 `severity` 返回成 `"严重"` / `"not_applicable"` / `"2"` 等各种形态，
裸 `int()` 会直接抛 `ValueError` 打挂整个 case（实测 30 条挂 2 条）。
`rubric.coerce_severity()` 在所有「模型输出进入系统」的边界上统一归一到 0-3。

### 附：模型输出的结构容错

同一类问题在 158 条全量上打挂了 3 条 case（`subject_004`、`subject_023`、`visual_029`）：
Keye 偶发把 `constraints` 输出成**字符串数组**而不是对象数组，下游 `c.get("id")` 直接
`AttributeError`。`_coerce_constraints()` 把字符串补成最小可用的约束体，非法项丢弃。

**通则**：凡是「模型返回的 list 里的元素要当 dict 用」的地方，都必须先 `isinstance` 过滤。
可以用一段 AST 扫描把这类位置一次性找出来：遍历所有 comprehension，
若循环变量被 `.get(` 调用而 generator 的 `ifs` 里没有对它的 `isinstance` 判断，即为隐患点。

### 附：bbox 尺度容错（曾让整条通道静默失效）

提示词要求 bbox 用 0~1 归一化，但 Keye 常按 Qwen2-VL 的习惯回**整数坐标**
（实测返回 `[145, 137, 435, 489]`）。老代码把它 clamp 到 0~1，于是
`x1=1.0, x2=144.94`，ffmpeg 的 crop 表达式随即失败——**定位器明明挑出了 4 个目标，
却一个都没比上**，而且没有任何报错，只表现为「区域通道判 pass」。

`integrity.normalize_bbox()` 统一折算：≤1.5 视为已归一化，≤1000 按千分比，
更大的量级按最大值折算。这类"静默失效"比崩溃更危险 —— 通道看起来在跑，
指标也不报错，只是永远给不出正例。

### 附：一致性区域通道的三个定位器缺陷

区域放大通道能跑起来之后，逐 case 看 `sub_results` 又暴露三个都会让通道白跑的问题：

1. **拿同一帧自比**：定位器给出 `time_a = time_b = 0.0`，于是「切镜后手表样式变化」
   这条正例必然判一致。→ 代码强制两个时刻分开（`CONSISTENCY_REGION_MIN_GAP`），
   太近就沿切镜或全片两端重新取。
2. **跨切镜的一对被间隔门槛误杀**：切镜前 1.0s 与切镜后 2.32s 只差 1.32s，
   却正是判「跨镜头连贯」最该看的一对。→ 间隔门槛**只对同镜头内**的比对生效。
3. **只挑人物部件，不挑背景**：标注里「单镜头内书架变成窗户」「背景沙发换款式」
   占相当比例，而定位器一律只挑脸/手表/衣服纹理。→ 提示词要求至少挑 1 个背景陈设目标。

另外把检测到的**切镜时刻直接喂给定位器**（`已知切镜时刻（秒）：[...]`），
让它按「一个在切镜前、一个在切镜后」选点，而不是自己猜。

### 附：定位框只能算"大致对"

实测让它框"脸"，框出来的是头顶。文字通道框错了顶多读不到字，
人物部件框错了会把两个时刻裁到不同部位、**凭空造出漂移**。
所以一致性裁剪的边距（`CONSISTENCY_REGION_PAD=0.14`）比文字通道松得多，
牺牲一点放大倍率换「目标一定在框内」。

### 附：静默失效比崩溃危险得多（`smoke_scan.py`）

`analyze_case` 里五个专项检测器各自包在 `try/except` 中——单个维度挂掉不该带走整条 case。
代价是：一次误删模块级常量导致的 `NameError` 被**五个 except 全部吞掉**，
于是流水线表面上正常跑完、每个维度都给 5 分「没问题」，唯一的迹象是
单条耗时从 ~150s 掉到 ~50s，而离线指标只表现为「召回突然归零」。

`smoke_scan.py` 用一条 case 做前置断言：包裹五个 scan 方法，要求它们
**都被调用、都不抛异常、都不返回 None**，且 `report` 里五个 scan 字段都非空。
跑全量评测前先过这一关，几分钟成本换掉一次 75 分钟的空跑。

```bash
python3 smoke_scan.py && python3 eval_f1.py --all --workers 5 --refresh --tag <tag>
```

同类教训：改动模块级常量区时不要用字符串切片删代码。可以用 AST 自查
「引用了但没定义」的模块级名字（注意 `CURATED_CASES: dict[...] = {...}` 是
`AnnAssign`，只扫 `ast.Assign` 会漏）。

---

## Agent 编排逻辑

### 固定工作流 + 有界 Agent 循环

主函数 `analyze_case()`（`judge_pipeline.py` line 917）是一条线性 pipeline，但包含丰富的条件分支：

1. **顺序执行**: 步骤①-⑦按序执行，每步的输出传给下一步

2. **条件分支**:
   - 原生视频 vs 帧回退: 视频 > `VIDEO_MAX_MB` 时，步骤④ 改为发帧，步骤④b 跳过
   - Zoom 跳过: 设置 `SKIP_ZOOM` 环境变量可跳过步骤④b（用于批量场景省钱）
   - 音频复检: 仅当 observer 音频置信度低时才触发步骤④c
   - 专科扫描开关: `COLLAPSE_SCAN`、`VISUAL_SCAN`、`TEXT_SCAN` 可各自独立禁用
   - 自适应崩坏投票: 首票明确自信时直接短路剩余投票
   - Live vs Mock: Gemini 未配置或失败且 `allow_fallback=True` 时回退到 MockVLM
   - VLM 失败处理: `allow_fallback=False`（默认）时报错，不伪造 mock 判定

3. **唯一真正的 Agent 循环**: `GeminiZoomInspector.inspect()` -- 模型可主动请求更多帧（`next_requests`），上限 `ZOOM_MAX_ROUNDS=2` / `ZOOM_MAX_FRAMES=4`

4. **缓存**: 结果按 `(case_slug, source, engine, mode, CACHE_VERSION)` 缓存在 `runtime/results/*.json`。当前 `CACHE_VERSION = "v17"`（line 894），升版本号即可失效旧缓存

5. **事件发射**: 每步通过 `on_event` 回调发射事件，供 SSE 实时 UI 更新

6. **Trace 持久化**: 每次模型调用的 system prompt、输入、原始输出、解析后的 JSON 都会保存到 `runtime/traces/<run_id>.json`，base64 图片替换为紧凑描述符以控制文件大小

---

## Agent 输入输出

### 输入
- **Case ID**（`<collection>/<case_name>`）
- **Source**（`kling` 或 `seedance`）-- 评估哪个视频
- **Mode**（`auto` / `T2V` / `I2V`）-- 自动检测: 有参考图则判为 I2V
- **配置**: Gemini endpoint/API key/model（环境变量或 `gemini.md`）
- **每案例文件**: `prompt.json`（prompt_zh、prompt_en）、`Kling*.mp4`、可选 `seedance*.mp4`、可选输入/参考图

### 输出
- `final.status`: `pass` / `needs_review` / `blocked`
- `final.summary`: 一行中文结论
- `scores`: 各维度分数（dynamic、visual、prompt、audio）
- `score_total`: 加权总分（0-100）
- `hard_failures`: 关键问题列表
- `collapse_level`: 0-3
- `rubric`: 8 叶 rubric 明细（verdict、severity、evidence、agreement status）
- `evaluations`: 逐条约束判定
- `dim_scores`: collapse/visual/text 的 1-5 分
- `evidence`: 带时间戳帧的证据卡（部分带红 bbox）
- `cost`: token/调用/成本明细
- `llm_trace`: 完整步骤级模型 I/O trace
- `audit_trace`: 简化步骤摘要
- `disagreements`: observer-verifier 分歧（需人工复审）
- `veto_reasons`: 否决原因（如适用）
- `report_html_path`: 独立 HTML 报告路径

---

## 本地 Keye 实测结果（每个自测集前 3 + 后 3，共 30 条）

三轮迭代，判定口径固定为「对口叶子 violated 且 severity>=2」：

| 维度 | 测试集 | baseline F1 | v2 F1 | **v3 F1** | v3 P / R | v3 特异度 |
|------|--------|------------|-------|----------|----------|----------|
| collapse | 明显崩坏 | 0.000 | 0.500 | **1.000** | 1.00 / 1.00 | 1.00 |
| instruction | 核心指令遵循 | 0.889 | 0.889 | **0.909** | 0.83 / 1.00 | 0.00 |
| consistency | 主体时序一致性 | 0.000 | 0.400 | **0.500** | 1.00 / 0.33 | 1.00 |
| text | 文字不可读 | 0.400 | 0.400 | **0.500** | 1.00 / 0.33 | 1.00 |
| visual | 曝光颜色异常油腻感 | 0.400 | 0.400 | **0.400** | 0.50 / 0.33 | 0.67 |
| **macro-F1** | | **0.338** | **0.518** | **0.662** | | |
| 崩溃/失败 case | | 2 | 1 | **0** | | |

单 case 约 1.5-3.5 分钟，30 条 5 路并发约 11 分钟。

三轮各自的关键变更：
- **baseline → v2**：解除专项扫描的 native 门控（Keye 只接图片，原门控让 5 个扫描永远不触发）、
  分辨率预算、参考图压缩、分块扫描、`coerce_severity` 容错、一致性改成对比对。
- **v2 → v3**：崩坏改为**运动峰值处的紧邻帧（0.12s）对比**、验证器输入瘦身
  （修掉纯文本请求 31k token 撑爆上下文）、文字大小写豁免提到铁律位置、
  一致性加「可见性变化 ≠ 陈设被替换」豁免。

### 前三轮（30 条小样本）的剩余缺口，及后续如何解决

当时三个维度的漏报都指向**目标太小 / 判断太主观**，超出抽帧粒度：

1. **text 漏报**：模型报「未发现清晰醒目的生成文字」。文字在小型复古收音机面板、
   设备侧面标识上，长边 1280 的整帧里只占几十像素，缩放后已不可辨。
   单纯加帧数无效（帧数 10→15 没有改善）。
   → **已解决**：`scan_text_regions()` 定位 + 裁剪放大通道（④b-vi'）。
   → 后来还发现更主要的根因是**判定**而非**看见**：模型抄出 `RIAA BAEY` 却说"拼写正确"，
     由转写合法性通道解决。
2. **consistency 漏报**：标注是「切镜后**手表样式**变化」「单镜头内**人物衣服纹理**改变」，
   同样需要主体区域裁剪后再成对比对。
   → **已解决**：`scan_consistency_regions()`（④b-ii'）。注意它上线后还要再修
   三个定位器缺陷（同一帧自比 / 跨切镜被间隔门槛误杀 / 只挑人物不挑背景）才真正生效。
3. **visual 偏低**：这一维度标注主观性最强。`judge_visual_032` 标注为「无」，
   但模型描述出的「面部油光、蜡质高光、塑料质感」正是该维度的正例定义 ——
   属口径分歧。→ 部分解决：`_lift_by_frames` 让帧级证据不被"整体观感"抹平；
   口径分歧仍建议先对齐标注。
4. **instruction 特异度 0.00**：该测试集 28 条里只有 6 条负例，前 3+后 3 只抽到 1 条负例，
   特异度不可估。→ **必须跑全量或按正负均衡取样**才能评估误报。

### 小样本的方差有多大（重要）

每维度只有 6~10 条时，**单条翻转就是 ±0.15~0.22 F1**。实测同一套代码、
temperature 0.2 下重复跑，`judge_text_001` 与 `judge_broken_002/003` 会在
TP/FN 之间来回翻。同一批 50 条上三轮的 macro-F1 分别是 0.686 / 0.733 / 0.701 ——
**轮次之间的差异不可解读**，只有机制层面的证据（某条漏报的根因被消除、
回放全部 case 后 P/R 的变化方向）才是可信的。结论务必落在全量 158 条上。

### 建议的下一步

- **对齐 visual 维度标注口径**后再调 `visual_greasy` 的 severity 阈值 ——
  这一维度的分歧已经不是模型问题（`judge_visual_032` 类 case）。
- **消化 text 的残余误报**：`text_028` / `text_032` 里模型把文字**正确抄了下来**
  却同时声称「模糊、无法辨认」+「字形崩坏」，对冲式措辞让文本规则无法与真正例
  （`text_001` 的 `SINHE` 首字母扭曲）区分。可考虑引入「同一处文字多次转写是否一致」
  作为新的确定性信号。
- **崩坏的 025/026 类误报**：「面部被截断」「狐狸尾巴消失」在文本上与真崩坏无法区分，
  需要更强的视觉证据（例如对该 bbox 做裁剪放大二次确认）。
- **阈值旋钮**：`JUDGE_COLLAPSE_PAIR_MIN_HITS`（1=偏召回，默认；2=偏精确）、
  `dim_scores` 的 T1~T4 并扫（visual 在 T4 上明显优于 T2，说明分档信息比二值更有信息量）。
- **每次改动后先跑 `smoke_scan.py`**，再跑全量。

---

## 第二条流程：Gemini agentic（`agent_judge.py`）

固定流水线是为 **Keye** 的短板设计的。同样 58 条上换成 Gemini 后，实测形态完全不同：

| | Keye 固定流水线 | Gemini 固定流水线 |
|---|---|---|
| 召回 | 0.850 / 0.737 | **1.000 / 1.000** |
| 特异度 | 0.625 / 0.667 | **0.375 / 0.333** |
| MCC | 0.475 / 0.359 | 0.548 / 0.525 |

Gemini **一条不漏**，连 Keye 上了区域放大通道都拿不到的「切镜后手表款式变化」
「书架变成窗户」都能指出来；但它几乎不说「没问题」。核心指令遵循那一集它把
**28 条全判成有问题**（TN=0），F1 0.880 恰好等于「无脑全判有」的基线，MCC = 0.000 ——
那个 F1 不含任何判别信息。

所以给 Gemini 堆召回通道是纯浪费。第二条流程反过来做：**不预先做任何感知，
把感知全部变成工具，让它自己决定看什么；判定侧加压来换特异度。**

### 工具清单

| 工具 | 花像素预算 | 作用 |
|---|---|---|
| `probe_motion` | 否 | ffmpeg 逐帧运动量分析 + 运动峰值与**运动低谷**时刻 |
| `probe_exposure` | 否 | ffmpeg 全片曝光/色彩统计量 + 经验分界参考（visual 必调） |
| `get_video_facts` | 否 | ffprobe 时长/分辨率 + 切镜检测 |
| `get_criteria` | 否 | 取某维度的判定口径与豁免清单 |
| `ask_keye` | 否 | 把窄问题委派给本地 Keye，拿回**纯文本**结论 |
| `watch_full_video` | 是 | 看整片 |
| `watch_segment` | 是 | 密集看某个时间段 |
| `sample_frames` | 是 | 抽指定时刻高清帧 |
| `zoom_region` | 是 | 裁剪放大某块区域 |
| `compare_times` | 是 | 两个时刻并排比对（可只比某块区域） |
| `submit_verdicts` | — | 交卷，结束循环（要过 `_finish_guard` 校验） |

三个设计要点：

1. **首轮不给画面**（`AGENT_EAGER_VIDEO=0`）。连「看视频」都是它自己调的工具，
   流程里不存在任何硬编码的感知步骤。好处是**预算随难度自适应**：
   实测负例 `subject_029` 4 轮 / 28k token / 33s 收敛，难例 `subject_002`
   花到 15 轮 / 189k token —— 固定流水线做不到这种分配。
2. **ffmpeg/ffprobe 类分析全部是「不花像素」的工具**。运动量峰值、切镜时刻这些
   本来就是确定性计算，让 agent 免费拿来决定「该看哪几秒」，比预先抽一堆帧划算得多。
3. **把 Keye 作为可委派的感知工具**。分工依据是实测出来的能力差异：Keye 判
   「相邻两帧之间发生了什么变化」很准、判「单帧结构是否正常」很差。所以
   `ask_keye` 的描述里直接写明了这个特点，要求按「A 到 B 之间某对象有没有异常变化」
   提问。它的回答以纯文本回到 Gemini —— **委派感知不占 Gemini 的像素预算**。

### collapse：误报全在肢体末端，只能用确定性策略层挡

collapse 的短板一直是特异度（0.400）。把模型报「有崩坏」的 26 条按证据构成拆开：

| 证据构成 | TP | FP | 精确率 |
|---|---|---|---|
| 含非肢体证据（面部/躯干/整体/物体/凭空消失） | 17 | 0 | **1.000** |
| 只有肢体末端（纯手指、纯腿脚解剖） | 3 | 6 | **0.333** |

模型偏爱报手（26 条里 16 条提到手/腿），但纯手部结论**比抛硬币还差**。
而且两类在文字上完全无法区分 —— TP 写「手指熔合粘连成一块」，
FP 写「无名指和小指熔合在一起」，用词一模一样。**这不是改提示词能解决的。**

做法是 `_apply_severity_policy()`：一个判定之后的确定性策略层，
把「只有肢体末端证据」的 collapse 判定从 sev>=2 降到 sev=1。三个设计要点：

1. **不用正则去猜证据串**，而是给 `submit_verdicts` 加结构化字段
   `affected_parts`（枚举：手或手指 / 脚或腿 / 面部或五官 / 躯干或全身 /
   物体或道具 / 背景或环境 / 主体数量）。正则太脆，模型换个措辞规则就失效。
2. **刻意不把降级规则写进提示词**。告诉模型「只报手会被降级」，它会为了保住结论
   多填一个部位，`affected_parts` 就失去信息量了。
3. **降级而不是清零**，被降的 case 仍以 sev=1 留在报告里可查。
   没填部位时不降级 —— 信息缺失时宁可放过。

实测（全量 34 条，`JUDGE_AGENT_LIMB_POLICY` 控制）：

|  | P | R | F1 | 特异度 | MCC | TP/FP/FN/TN |
|---|---|---|---|---|---|---|
| 关闭 | 0.769 | 0.870 | **0.816** | 0.400 | 0.303 | 20/6/3/4 |
| 开启 | **0.882** | 0.652 | 0.750 | **0.818** | **0.440** | 15/2/8/9 |

**这是操作点选择，不是修 bug**：买精确率、卖召回，绝对条数上是「6 个真正例换
5 个误报」，接近 1:1；MCC 涨是因为负例只有 11 个而正例 23 个，TN 在平衡指标里
权重更大。F1 反而降，因为正例占 68%、F1 天然奖励多报。
当前默认开启（QA 场景误报直接消耗审核人力）；若评测口径只看 F1 就关掉。

#### 回放预测错了，教训值得单独记

上线前我用正则给历史证据串分类做回放，预测 F1 0.850 / 特异度 1.000，
**实测 0.750 / 0.818**。原因：`broken_022` 在上一轮的原话是
「**下半身**结构崩坏，双腿融合成肉块」，「下半身」被正则算作非肢体证据而保留；
换成结构化枚举后模型把它填成 `["脚或腿"]`，同一个 case 就落进了降级桶。

**那次回放测的是正则的类别边界，不是这条策略本身。**
用文本正则回放一个后来要靠结构化字段实现的规则，两者边界不一致，结论不可迁移。
要么用最终实现去回放，要么就直接实跑。

### 判定口径要内联进系统提示，不要做成工具

原先口径是 `get_criteria(dimension)` 工具，一次读一个维度。看着很「agentic」，
实际有两个代价：

1. **读全 5 个维度要花 5 轮 / 16 轮上限的 31%**。轮次是硬约束，而口径是完全静态的
   文本，没有任何理由跟取证抢预算。实测 `visual_013` 就是 16 轮用尽（其中 14 次
   zoom + 读口径），静默变成五维全零。
2. **「读口径」变成了可选动作**，模型经常不读就下结论。

改成内联到系统提示末尾后的 A/B（同一份代码，只差这一处）：

| 维度 | 口径做成工具 | 口径内联 | 变化 |
|---|---|---|---|
| visual | 0.750（R 0.600） | **0.818**（R 0.720） | **+0.068** |
| collapse | 0.816 | 0.816 | 0.000（TP/FP/FN 两轮都是 20/6/3） |

**这个对比恰好把「规则挤占预算」和「模型看不出来」两种失效分开了**：

- visual 本来被预算饿着（`_finish_guard` 强制它调 `probe_exposure` + `zoom_region`，
  再加 5 轮读口径就没什么空间了），白送 5 轮就涨了 0.068；
- collapse 一动不动，因为它的漏报不是规则问题 —— 三轮实测里 collapse 漏报只有
  **1/3** 援引豁免条款，另外 2/3 是「核对过了，没发现」的纯感知失败。
  给它再多轮次、把规则摆在眼前都没用。

**教训：判断「规则太多是不是在拖累模型」，不要看规则字数，要看规则以什么形式
占用什么预算。同样 4641 字，内联是免费的，做成工具就要花掉三分之一的轮次上限。
另外别把「感知失败」误诊成「规则失败」—— 前者要换证据形式（工具、分辨率、
客观数值），后者才是改文字能解决的。**

### 五维度实测结果（全量 158 条，Gemini agentic vs Keye 固定流水线）

| 维度 | N | P | R | **F1** | 特异度 | MCC | Keye 固定流水线 F1 |
|---|---|---|---|---|---|---|---|
| 核心指令遵循 | 28 | 0.952 | 0.952 | **0.952** | 0.833 | 0.786 | 0.829 |
| 主体时序一致性 | 30 | 0.947 | 0.947 | **0.947** | 0.900 | 0.847 | 0.829 |
| 文字不可读 | 32 | 0.941 | 0.727 | **0.821** | 0.900 | 0.583 | 0.667 |
| 明显崩坏 | 34 | 0.882 | 0.652 | **0.750** | 0.818 | 0.440 | 0.816 |
| 曝光颜色油腻感 | 34 | 0.947 | 0.720 | **0.818** | 0.875 | 0.516 | 0.550 |
| **macro-F1** | | | | **0.858** | | | 0.738 |
| **macro-MCC** | | | | **0.634** | | | — |

三个维度的短板与原因：

- **collapse 特异度只有 0.455**（6 条误报）。误报集中在手部：模型对手/手指的
  结论精确率只有 0.545，而不涉及手的结论有 0.889。它偏爱报手（20 条报「有」
  里 11 条是手），但在手上的判断接近抛硬币。另有两条是「物理逻辑崩坏」
  （切片器不动却不断出现薄片、滑雪站姿左右腿互换）—— 这类模型认为是缺陷、
  标注员没算的争议项。
- **visual 召回 0.600**（10 条漏报）。已经从 0.130 拉到 0.600，剩下的漏报是
  亮度落在经验分界另一侧的正例，客观数值帮不上，只能靠像素判断。
- **text 召回 0.727**。残余误报 `text_028`/`text_032` 与真正例在文本特征上
  无法区分（模型同时给出「模糊」与「字形崩坏」两种理由），记为已知残余。

### visual：模型的美学先验会盖掉观察结论，得用客观数值把它拉回来

这是五个维度里最难的一个，也是唯一一个「改提示词完全无效」的。
初始状态：23 个正例漏了 20 个（F1 0.222 / R 0.130），漏报理由高度雷同：

> 「画面光影质感良好，**符合电影感和温暖金色灯光的设定**，无明显噪点、过曝或水印」
> 「画面整体质感**符合赛博朋克 CG 风格**，未发现明显异常」

两个根因：

1. **它在拿「好看」当「没问题」**。而标注员说的「油腻感」恰恰长这样 ——
   暖橙滤镜、高饱和高对比、皮肤打蜡般的高光。模型把这个缺陷的典型外观
   当成了美学优点，还引用提示词给自己背书（「符合设定」）。
2. **证据分辨率不够**。它几乎只调 `watch_full_video`（低 fps + 降分辨率），
   而油腻感是**皮肤微观质感**问题，在那种画面里根本不可见。

**改口径无效**：明确写了「符合提示词氛围不是理由」之后，`visual_013`
zoom 了两次、还问了两次 Keye，结论依旧是「符合提示词要求的暖色调」——
原话搬回来。这不是指令没写清，是先验压不住。

真正起作用的是**给它客观数值**。离线实验（不打 API）发现「油腻感」标注与
全片平均亮度高度相关：

| 特征 | AUC |
|---|---|
| 全片平均亮度 `y` | 0.087（反向 → **0.913**） |
| 高光均值 `yhigh` | 0.091（反向 → 0.909） |
| 饱和度 | 0.630 |
| 对比度 | 0.385 |

8 个负例的 `y` 全部 >= 120.9，26 个正例里 23 个 < 120.9。留一法（阈值只在
其余 33 条上拟合）P 0.958 / R 0.885 / F1 0.920 / 特异度 0.875 —— 而
「无脑全判有」的基线是 F1 0.867 但**特异度 0.000**，所以这不是在吃类别不平衡。

机理上讲得通：重调色、暗部压死配合高光发光的片子最容易被判油腻，
而「欠曝」本身就在这个维度的定义里。

**怎么用**（关键：没有把阈值写成判决）：
- 新工具 `probe_exposure` 返回客观数值 + 经验分界作为**先验参考**，
  并明确标注「仅 8 个负例的小样本规律，不是判决」，模型仍要在像素上
  找到对应的具体现象。
- `_finish_guard` 硬约束：要判 visual 没问题，必须先调过 `probe_exposure`
  **且** zoom 过人脸/材质，否则打回重做。光靠提示词写「必须先调」它会跳过。

结果：**F1 0.222 → 0.750，精确率 1.000，特异度 1.000（零误报），MCC 0.007 → 0.516**。
翻转案例 `visual_022` 的证据直接引用了数值：「全片平均亮度 y=87.67 偏低，
对比度过强导致暗部死黑（2.5s 驾驶舱内部完全丢失细节）」。

**教训：当模型的失效来自它自己的先验（审美、常识、风格偏好）而不是看不清时，
改提示词基本无效。要找一个它无法反驳的客观量，做成工具喂给它。
同时留意别把小样本拟合出来的阈值直接当判决 —— 只当先验，让模型去找对应证据。**

### 豁免清单会被反向滥用（collapse 的 7 条漏报）

写豁免清单的本意是压误报，但模型会把「X 不算问题」读成「X 附近的一切都不算问题」。
collapse 的漏报理由几乎一模一样：

> 「小机器人存在**变形**，但属于快速运动导致的运动模糊（**已豁免**）」
> 「手部因运动模糊**看不清**（属豁免情形）」

而崩坏恰恰最常发生在运动中，于是这条豁免把最该抓的地方全放过了。两处修法：

- **口径层**：明确「模糊是『糊』，崩坏是『错』—— 模糊会让边缘变软，但不会让手指
  数量变多、不会让两只手长在一起」；并且「看不清 ≠ 没问题」，看不清要去找清晰帧。
- **工具层**：`probe_motion` 除峰值外还返回 `calm_times`（运动低谷）。
  原先它只给峰值，描述里还写着「崩坏几乎只发生在运动峰值附近」，
  直接造成两种失效 —— 只查那几个点，以及在一堆模糊帧里确认不了就放弃。
  现在的用法是「峰值发现疑点 → 最近的低谷核对结构」。

**教训：豁免条款要写清『豁免的是什么』，还要写清『豁免不能推广到什么』；
并且每条豁免最好配一个可执行的替代动作（这里是「去 calm_times 核对」），
否则模型会拿豁免当放弃调查的借口。**

**但要注意：堵住一个借口它会换一个。** 收窄运动模糊豁免之后，同一批漏报的
理由整体迁移到了**画风**上 —— 「手指较长（符合动漫风格）」「结构符合卡通风格」
「水墨质感，未发现违背该风格的崩坏」（9 条漏报里 4 条）。
所以补了「画风只决定正常长什么样，不决定可以画错，判断标准是在该画风内是否自洽」。
这类「找理由放行」的行为不是靠堵单个借口能解决的，要把**判断标准本身**讲清楚。

另外这一轮的教训是**别一次改太多**：同时改了「明显的标定尺度」和「豁免方向」
两处之后，collapse 的 F1 从 0.732 掉到 0.651（误报 4→6、漏报 7→9，两个方向
都反了），无法归因到具体哪一处。已回退到原口径，只保留画风条款这一条窄改动。

### 轮次耗尽会静默变成「五维全无问题」（又一个静默失效）

工具循环跑满 `max_rounds` 还没调 `submit_verdicts` 时，原先直接返回
`{"_unfinished": True}`。上层是从 `submit_verdicts` 的参数里取每个维度的 severity 的，
返回里没有维度字段 → 全部取默认值 0 → **五个维度一起变成「没问题」**。

危险的地方在于它在评测里**和一次真实的「判无问题」完全无法区分**：
不报错、不打日志、`pred=无`，看起来就是一条正常的负例判定。
实测 `judge_visual_013` 正是这样丢的 —— 它 zoom 了 14 次、把 16 轮全部用完，
证据字段是空字符串，却被记成了一条干干净净的假阴性。
**查得越投入的 case 越容易这样丢**，而这类 case 往往正是真的有问题的那些。

两道防线：

1. **强制交卷**：轮次用尽后再发一轮，工具表收窄到只剩 `submit_verdicts`
   （从机制上断掉「再查一个」的选项），要求它用已掌握的信息立刻给判定。
2. **兜底报错**：连强制交卷都失败才返回 `_unfinished`，此时
   `analyze_case_agentic` 直接 `raise` —— 宁可整条记为失败，也不能伪装成负例。

**教训：凡是「拿不到结果就取默认值」的路径都要审一遍默认值是什么。
如果默认值恰好是某个合法的判定结果，那这条路径就是一个静默失效。**

### 「明显」需要标定尺度，否则放大工具会自己制造误报

collapse 的 4 条误报全部来自 `zoom_region` 放大后的解剖学细节：手指关节形状、
车轮位置偏内、滑雪板轮廓变形、手臂与长椅边缘穿模。裁出来放大到几百像素逐帧盯，
**任何**手和物体轮廓都会显得有点怪。而标注口径的「明显」是「正常速度播放、
看整帧，一眼能看出」。

修法是给放大工具划定用途边界：**放大只用于确认你在整帧里已经看到的疑点，
不用于发现新疑点**；只有放大后才「品」出来的瑕疵最多 sev=1。
实测有效 —— `broken_029` 修好后 zoom 了 10 次仍然判 sev=0。

### 工具循环的三个坑

- **Gemini 3 会返回只含 `thoughtSignature` 的 part**，原样回传会被判成
  「没有 parts」而 400。回传模型回合时必须剔掉空 part，但挂在 `functionCall`
  上的 `thoughtSignature` 要保留，否则多轮工具调用失效。
- **`functionResponse` 装不了媒体**，而这些工具返回的正是图片。绕法是在同一个
  user content 里同时放 `functionResponse`（结构化结果）和媒体 part。
- **多轮比单次更怕瞬时故障**：轮次越多，至少踩中一次 429/503/超时的概率越高，
  而一次抖动会连带浪费前面几轮已经花掉的 token。`_post_with_retry` 给每一轮
  单独加退避重试。注意 socket 超时是**裸 `TimeoutError`**，消息只有 `timed out`，
  不匹配任何文本规则 —— 实测 58 条里 5 条失败有 3 条就是它。
- **历史里的媒体会被逐轮重复上传**（最贵的一个坑）。多轮 function calling 是无状态的：
  每一轮都要把整个 `contents` 重新 POST 一次，所以第 1 轮塞进去的整片视频会在
  第 2..N 轮各传一遍。一条 15 轮的 case 等于把同一个 70MB 视频传了 15 次 ——
  这才是 `TimeoutError` 连续 4 次重试全挂的真正原因（加重试只能缓解抖动，
  治不了这个）。
  本网关是 Vertex 风格 endpoint，**没有 Files API**（`/upload/v1beta/files`、
  `/v1beta/files` 全部 404），拿不到 `file_uri` 复用，只能在客户端侧收上下文：
  `evict_stale_media()` 只保留最近 `GEMINI_TOOL_KEEP_MEDIA`（默认 4）件媒体，
  更早的换成一句文字占位。
  **必须留文字占位、不能直接删 part**：删掉模型会以为自己没看过那个位置，
  于是重复调同一个工具；留一句「画面已移除，描述见上文」它就知道去自己的
  历史发言里找结论。第 1 轮不驱逐（入参媒体要完整看到一次）。
  实测：`subject_002` 189k→108k token 且仍抓到 sev=3；原先 4 次超时报废的
  `subject_014` 变成 5 轮 / 36k / 42s 正常出结论。

```bash
python3 eval_f1.py --dims consistency instruction --all --provider gemini_agent
```

---

## 离线 P/R/F1 评测（eval_f1.py）

五个 P0 自测集的量化评测入口。它把「跑 pipeline」和「算指标」拆开，
所有中间结论落盘，便于逐条追 badcase。

```bash
# 每个测试集前 3 + 后 3 条，5 路并发，忽略缓存
JUDGE_SKIP_EVIDENCE=1 python3 eval_f1.py --head 3 --tail 3 --workers 5 --refresh --tag v3

# 全量 158 条
JUDGE_SKIP_EVIDENCE=1 python3 eval_f1.py --all --workers 5 --tag full

# 只跑指定维度
python3 eval_f1.py --dims text collapse --head 3 --tail 3
```

**数据源**：五个自测集目录（`../I2VA*`）+ 五个标签 xlsx（`../自测集_*.xlsx`）。
标签列为「标签」，**以「无」开头即负例**，其余（有明显崩坏 / 主体一致性 /
动作文本响应 …）均为正例。五个集共 158 条，正例占比 65%~79%。

**预测口径**：`report["dim_badcase"][dim]["badcase"]`，即该维度对口 rubric 叶子里
出现 `violated` 且 `severity>=2`。同时对 `report["dim_scores"][dim]`（1-5 分，5=好）
做 `score<=T` 的阈值并扫，便于挑更优判据。

**产物**（`runtime/eval/`）：
- `f1__<tag>__<时间戳>.jsonl` —— 逐条预测（gt / pred / severity / 命中叶子 / 专项检测证据）
- `f1__<tag>__<时间戳>.txt` —— 汇总表 + 阈值并扫 + **误判明细**（漏报/误报逐条列出标注与模型证据）

**注意**：`CACHE_VERSION` 变更后旧缓存全部失效，评测会冷启动重跑。
调 prompt 或判定逻辑后务必 bump `CACHE_VERSION`，否则会读到旧结论。

---

## 关键文件索引

| 文件 | 用途 | 关键行 |
|------|------|--------|
| `judge_pipeline.py` | 主编排 | `analyze_case()` line 917；`GeminiObserver` line 640；`GeminiZoomInspector` line 690；`GeminiVerifier` line 920；`build_cost()` line 855；`CACHE_VERSION` line 894 |
| `integrity.py` | ffmpeg/ffprobe 工具 | `inspect_video()` line 25；`check_integrity()` line 62；`motion_profile()` line 137；`plan_sample_times()` line 196；`sample_frames_motion_aware()` line 232；`extract_native_frames()` line 113 |
| `rubric.py` | 固定 8 叶 rubric | `RUBRIC` line 15；`normalize_rubric()` line 90；`rubric_hard_failures()` line 165；`disagreements()` line 210 |
| `rubric_prompts.py` | 所有 system prompts | `PROMPT_COMPILER_SYSTEM` line 1；`OBSERVER_SYSTEM` line 44；`ZOOM_SYSTEM` line 130；`COLLAPSE_VOTE_SYSTEM` line 180；`AUDIO_RECHECK_SYSTEM` line 260；`VERIFIER_SYSTEM` line 305；`VISUAL_SCAN_SYSTEM` line 290；`TEXT_SCAN_SYSTEM` line 360 |
| `scoring.py` | 评分/硬门/否决 | `MODE_WEIGHTS` line 15；`apply_veto()` line 32；`apply_rubric_scores()` line 72；`apply_hard_gates()` line 150；`weighted_total()` line 136 |
| `config.py` | Gemini 配置 | `load_gemini_config()` line 48 |
| `gemini_client.py` | Gemini API 客户端 | `encode_file()` line 18；`extract_json_object()` line 108；`GeminiClient.generate_json()` line 158 |
| `evidence.py` | 证据帧标注 | `build_evidence()` line 115；`annotate_frame_bbox()` line 57 |
| `trace_store.py` | Trace 持久化 | `TraceSession` line 44；`record_step()` line 55；`sanitize_parts()` line 16 |
| `report_html.py` | HTML 报告生成 | `write_report_html()` |
| `app.py` | HTTP 服务器 | `main()` line 697；`_stream_analyze()` line 293；`_job_start()` line 244 |
