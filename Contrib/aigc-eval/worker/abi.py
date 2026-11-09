"""AgentK capability 的对外契约：ID、队列名、Temporal 类型名与输入输出模型。

这个模块是 worker 的唯一契约来源——workflow、activity、verify 脚本都从这里取常量，
避免同一个字符串在多处各写一遍而漂移。

两条不显然的约束：

1. ``CAPABILITY_ID`` 必须匹配 ``contrib.<owner>.<name>``，每段都是 ``[a-z][a-z0-9_]*``。
   SDK 在 worker 启动时就会校验，写错要到启动才报错，所以不要改成中划线或大写。
2. ``WORKER_TASK_QUEUE`` 不能叫 ``agentk``：那是核心队列，每个 contrib worker 的注册
   workflow 都投递到它上面等核心 worker 处理。contributor 去轮询它就会截走这些任务，
   而本地没有对应的 workflow type，注册会连带别人一起失败。SDK 因此直接拒绝这个名字。

字段一律 camelCase，与平台上其他 capability（ffmpeg 的 ``videoUrls``/``blobKey``）保持一致，
免得调用方在同一个 session 里遇到两套命名风格。
"""

from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field

CAPABILITY_ID = "contrib.aigc_eval.video_judge"
WORKER_TASK_QUEUE = "contrib.aigc-eval"
WORKFLOW_TYPE = "AigcEvalVideoJudgeV1"
INSPECT_ACTIVITY = "aigc_eval.video_judge.inspect.v1"

# 质检的八个阶段各自一个 activity。
#
# 为什么要拆：Temporal 只能在 activity 边界上续跑。整条质检塞进一个 activity 时，
# 崩在第 90 分钟就得从第 0 分钟重来（模型级检查点能省钱，但抽帧、标框、报告组装
# 全要重做，而且重来一遍的窗口里再崩一次就又回到起点）。拆开之后，已完成的阶段
# 结果留在 workflow 历史里，重启后从下一个阶段接着跑。
#
# 名字带 .v1：activity 名是 workflow 历史里的一部分，改名会让在途的 run 重放失败。
# 要改语义就加新版本、让旧名继续存在，直到在途 run 跑完。
STAGE_ACTIVITIES: dict[str, str] = {
    "prepare": "aigc_eval.video_judge.prepare.v1",
    "compile": "aigc_eval.video_judge.compile.v1",
    "observe": "aigc_eval.video_judge.observe.v1",
    "scan": "aigc_eval.video_judge.scan.v1",
    "recheck": "aigc_eval.video_judge.recheck.v1",
    "aggregate": "aigc_eval.video_judge.aggregate.v1",
    "evidence": "aigc_eval.video_judge.evidence.v1",
    "report": "aigc_eval.video_judge.report.v1",
}

# 含模型调用的阶段。这些阶段的 activity 是**可重入**的：跑到需要模型时把请求交回
# workflow，由 workflow 走 invoke_capability 调完再带着响应重放（见
# ``aigc_eval.judge.model_relay``）。
#
# 其余三个阶段一次跑完：prepare 是纯 IO（下载、ffprobe、SSIM、抽帧），aggregate 是
# 纯计算，report 是组装加上传。它们没有模型调用，所以也不该付重放的代价——prepare
# 尤其不能重放，它是唯一可能跑到二十分钟的阶段。
MODEL_STAGES = frozenset({"compile", "observe", "scan", "recheck", "evidence"})

# 单个阶段允许的模型调用轮数上限（含解析失败后的重发）。
#
# 这是一道防御性栅栏，不是业务预算：正常情况下 4c 最多三次调用、每次最多四次尝试，
# 远到不了这个数。它存在的意义是万一重放逻辑出错（比如 digest 不稳定导致每轮都算出
# 新键），workflow 会停下来报错，而不是无限起子 workflow 把钱烧光。
MAX_MODEL_ROUNDS = 16

# Activity 把 on_event 阶段事件按批 signal 回 workflow，由 workflow 转成 report_progress。
# 之所以要中继：report_progress 只能在 workflow 里调，而质检是一个跑完 7 个阶段才返回的
# 整体调用，进度只能从它的回调里出来。
PROGRESS_SIGNAL = "aigc_eval.progress.v1"

CAPABILITY_DESCRIPTION = (
    "AIGC 视频质检：按 native8-v1 协议对生成视频做八维度、28 条子判据的逐项核查，"
    "定位具体问题的时间点与画面证据，产出结构化结论和一份可直接打开的 HTML 报告。"
    "支持 T2V / I2V / V2V；给出的是逐维度三档结论（无问题 / 瑕疵 / 恶劣错误）与"
    "「未核实」标记，不产出综合总分。"
)


class VideoJudgeInput(BaseModel):
    """质检入参。

    ``videoUrl`` 与参考素材都收 HTTP(S) URL；本地绝对路径仅在本机调试时可用，
    分布式部署下 worker 与调用方不共享文件系统，传路径必然取不到文件。
    """

    videoUrl: str = Field(
        description="待质检视频的 HTTP(S) URL；本机调试时也接受绝对路径。",
    )
    prompt: str = Field(
        description="生成该视频时使用的提示词，质检据此判断指令遵循情况。",
    )
    referenceImageUrls: list[str] = Field(
        default_factory=list,
        description="参考图 URL。I2V 场景下第一张按首帧参考处理。",
    )
    referenceAudioUrls: list[str] = Field(
        default_factory=list,
        description="参考音频 URL（配音、音乐等）。不改变生成模式，其内容与风格要求归指令遵循。",
    )
    sourceVideoUrl: str | None = Field(
        default=None,
        description="V2V 场景的源视频 URL。给了它即按 V2V 判定，并做源/生成对齐检查。",
    )
    mode: Literal["T2V", "I2V", "V2V"] | None = Field(
        default=None,
        description="强制指定生成模式。缺省按素材自动判定：有源视频即 V2V，否则有参考图即 I2V。",
    )
    provider: str = Field(
        default="agentk",
        description="模型通道，默认走平台内的 agentk 通道。",
    )
    refresh: bool = Field(
        default=False,
        description="忽略同输入的历史结果缓存，强制重跑（会重新产生模型开销）。",
    )


class StageInput(BaseModel):
    """阶段 activity 的入参：只有定位信息，状态本体在对象存储里。

    刻意不把状态放进来：Temporal 单个 payload 上限 2MB，而 4b 的事实底稿加观察器事实
    在长素材上很容易顶到那个量级，一旦超限就是整次质检失败，且失败点随素材长度漂移。
    """

    runKey: str = Field(description="本次 run 的状态命名空间，取 workflow run id。")
    fromStage: str = Field(description="读哪个阶段的输出作为输入。")


class StageResult(BaseModel):
    """阶段 activity 的回参：写到了哪儿 + 一点摘要，全部小体积。

    摘要进 workflow 历史，是排查"哪一段开始不对"时唯一能直接看到的东西。
    """

    runKey: str
    stage: str
    summary: dict[str, Any] = Field(default_factory=dict)


# ===========================================================================
# 平台能力的调用契约。
#
# 这几个模型描述的是**我们要调的别人**，不是我们对外的声明，所以不进
# VideoJudgeInput/Output 的 schema。写法参考 storyboard-example 的 abi.py。
#
# 输入侧用 extra="forbid"：拼错字段名要在本地就炸，而不是等平台回一个 schema 错误。
# 输出侧一律 extra="ignore"：平台随时可能加字段，forbid 会让一次已经花掉模型钱的
# 调用在解析回参时失败。
# ===========================================================================


class UnderstandMediaInput(BaseModel):
    """``agentk.understand_media``：给一组素材 URL 和一个问题，返回文字回答。

    ``mediaUrls`` 的 schema 写死 maxItems=10，降载在 activity 侧的 ``_select_media``
    里已经做过，这里到不了上限。
    """

    model_config = ConfigDict(extra="forbid")

    model: str
    mediaUrls: list[str] = Field(min_length=1, max_length=10)
    prompt: str


class UnderstandMediaOutput(BaseModel):
    model_config = ConfigDict(extra="ignore")

    text: str = ""
    model: str = ""
    params: dict[str, Any] = Field(default_factory=dict)


class GenerateTextInput(BaseModel):
    """``agentk.generate_text``：没有素材的调用走它（understand_media 的 mediaUrls
    是 minItems=1，走不通）。"""

    model_config = ConfigDict(extra="forbid")

    model: str
    prompt: str


class GenerateTextOutput(BaseModel):
    model_config = ConfigDict(extra="ignore")

    # 未给 outputSchema 时 result 是模型原文字符串；给了就是对象。两种都收，
    # 由 providers/model_call.raw_text_from_output 统一收敛。
    result: Any = None
    model: str = ""
    params: dict[str, Any] = Field(default_factory=dict)


class ModelStep(BaseModel):
    """可重入阶段 activity 的回参：要么给下一个请求，要么给最终结果。

    ``request`` 非空表示"阶段跑到某次模型调用前停住了"，workflow 该去调一次然后把
    响应带回来重放；为空表示阶段已经跑完，``result`` 有效。
    """

    request: dict[str, Any] | None = None
    result: StageResult | None = None


class ModelStepInput(BaseModel):
    """可重入阶段 activity 的入参。

    ``responses`` 是本阶段目前已经拿到的模型响应，按 digest 分组、组内按尝试顺序。
    它随每一轮增长，是重放能接着往下走的唯一依据。只装本阶段的（跨阶段的结果在对象
    存储里），所以体积受单阶段调用次数约束——最多是 4c 的三次，实测约 100 KB。
    """

    runKey: str
    fromStage: str
    responses: dict[str, list[dict[str, Any]]] = Field(default_factory=dict)


class DimensionVerdict(BaseModel):
    """单个维度的结论。

    ``tier``/``score`` 表达的是「没有确认到缺陷」，**不等于**「已核实无问题」：
    子判据全是 not_observable 的维度同样会得到 score=1。要区分两者只能看
    ``decisionStatus``（resolved / review_required）和 ``unresolvedSubcriteria``。
    调用方若只读 tier 就展示绿灯，会把「根本没看」显示成「没问题」。
    """

    id: str
    name: str
    applicable: bool
    tier: str | None = Field(description="三档中文标签；维度不适用时为 null。")
    score: float | None = Field(description="0 / 0.5 / 1；维度不适用时为 null。")
    decisionStatus: str = Field(description="resolved 表示已核实；review_required 表示尚有未核实项。")
    unresolvedSubcriteria: list[str] = Field(
        default_factory=list,
        description="该维度下未能核实的子判据 ID。非空即说明结论不完整。",
    )


class JudgeIssue(BaseModel):
    """一条确认的问题，带时间定位与三段式事实。"""

    id: str
    dimensionId: str
    subcriterionId: str | None = None
    title: str
    startSeconds: float | None = None
    endSeconds: float | None = None
    requirement: str | None = Field(default=None, description="该子判据的要求。")
    observed: str | None = Field(default=None, description="实际观察到的现象。")
    explanation: str | None = Field(default=None, description="为何按此档计。")


class JudgeArtifact(BaseModel):
    """一件产物，字段与 ``agentk.worker.Artifact`` 逐个对齐。

    不直接用 SDK 那个 dataclass：输出契约整体是 Pydantic 模型，``model_json_schema()``
    要能把它一起描述出来。字段名一旦对不上，AgentK 就不会把它当合法 artifact 提取，
    表现是 ``tool_exec_succeeded.artifacts`` 空着、而结果里明明有产物。
    """

    type: str = Field(description="产物类型，如 html / video / image。")
    uri: str = Field(description="产物地址，供下游直接访问。")
    description: str | None = Field(default=None, description="面向人的简短说明。")


class VideoJudgeOutput(BaseModel):
    """质检结论。

    刻意不含综合总分：native8-v1 取消它是为了不让某一维的确认缺陷被其他维度平均掉。
    需要单一门禁信号的调用方应自己定规则，例如「violatedSubcriteria>0 即不通过」，
    并把 ``unverifiedDimensions>0`` 当作「结论不完整、需人工复核」而非通过。
    """

    analysisStatus: str
    mode: str = Field(description="T2V / I2V / V2V")
    dimensions: list[DimensionVerdict]
    issues: list[JudgeIssue]
    violatedSubcriteria: int
    unresolvedSubcriteria: int
    applicableDimensions: int
    resolvedDimensions: int = Field(description="已核实到位的维度数。")
    unverifiedDimensions: int = Field(description="仍有未核实子判据的维度数。")
    unverifiedDimensionIds: list[str] = Field(default_factory=list)
    reviewRequiredCount: int = Field(description="被程序门禁拒收、需人工复核的条目数。")
    reportUrl: str | None = Field(default=None, description="HTML 报告链接。")
    reportJsonUrl: str | None = Field(
        default=None,
        description=(
            "完整报告 JSON 链接。本输出只是结论投影，要自行渲染详版报告"
            "（证据帧、子判据、审计轨迹）需取这一份；其中的帧已换成可公网访问的 URL。"
        ),
    )
    # 与 reportUrl / reportJsonUrl 同源，两份都留着：单独字段是既有调用方（studio 页面）
    # 在读的，artifacts 是平台约定——AgentK 只从这个名字的数组里提取产物放进
    # tool_exec_succeeded.artifacts，下游因此不必知道本 capability 的私有字段名。
    artifacts: list[JudgeArtifact] = Field(
        default_factory=list,
        description="本次产出的产物：HTML 报告与完整报告 JSON；未生成时为空。",
    )
    reportWarning: str | None = Field(
        default=None,
        description="报告生成或上传失败时的原因；结论本身仍然有效。",
    )
    cachedResult: bool = Field(
        default=False,
        description="true 表示命中同输入的历史结果，本次未产生模型开销。",
    )
