"""ffmpeg_tool 能力的 ABI：workflow/activity 名称与输入输出契约。

契约面向 agent-k：输入槽位接受 https URL / blob key（db:table:key）/
base64:xxx；媒体产物以可下载 URL 的 Artifact 返回，分析类结果在 result。
op 白名单与服务端 `ffmpeg/films/executor.py` 一一对应（29 个）。
"""

from typing import Annotated, Any, Final, Literal, get_args

from agentk.worker import Artifact
from pydantic import BaseModel, ConfigDict, Field, model_validator

CAPABILITY_ID: Final = "contrib.ffmpeg_tool"
TOOL_NAME: Final = "ffmpeg_tool"
PIPELINE_NAME: Final = "ffmpeg_tool"
WORKFLOW_TYPE: Final = "FfmpegTool"
WORKER_TASK_QUEUE: Final = "contrib.ffmpeg-tool"
INFER_ACTIVITY: Final = "contrib.ffmpeg_tool.infer"

FfmpegOp = Literal[
    "extract_frame",
    "fit_image",
    "freeze",
    "animate_image",
    "trim",
    "trim_av",
    "clip_av",
    "cut",
    "retime",
    "retime_av",
    "concat",
    "concat_av",
    "strip_audio",
    "add_silent_audio",
    "restore_audio",
    "extract_audio",
    "trim_leading_silence",
    "fit_audio_duration",
    "mix_bgm",
    "mix_audio",
    "crop",
    "keyint",
    "scale_short_edge",
    "overlay_region",
    "overlay_text",
    "title_card",
    "burn_subtitles",
    "probe",
    "scene_detect",
]
FFMPEG_OPS: Final[tuple[str, ...]] = get_args(FfmpegOp)

# 供 agent 阅读的 op 目录（写进 JSON Schema 的字段描述）
OP_CATALOG: Final = (
    "op 目录（op: 关键 params / 输入槽位）：\n"
    "extract_frame: t（秒），输入 video，产物 image；"
    "fit_image: w h color，输入 image；"
    "freeze: duration w h fps with_audio，输入 image，产物 video；"
    "animate_image: duration w h zoom_start zoom_end（Ken Burns 推近），输入 image；\n"
    "trim: duration（截断去音轨）；trim_av: duration（截断保音轨）；"
    "clip_av: start duration（精确切片）；cut: start end（流拷贝优先）；"
    "retime / retime_av: target_duration（变速对齐时长），以上输入 video；\n"
    "concat / concat_av: w h fps（无声/有声拼接），输入 videos；\n"
    "strip_audio / add_silent_audio / extract_audio: 无参，输入 video；"
    "restore_audio: 音轨贴回视频，输入 video + audio（或 video2）；"
    "trim_leading_silence: threshold_db min_silence，输入 audio；"
    "fit_audio_duration: target_duration，输入 audio；"
    "mix_bgm: bgm_volume voice_loudnorm，输入 video + audio(BGM)；"
    "mix_audio: offsets_ms gains，输入 audios（可选 video）；\n"
    "crop: x y w h；keyint: 无参（全关键帧转码）；"
    "scale_short_edge: short_edge fps；"
    "overlay_region: x y w h start end，输入 video + video2/image；"
    "overlay_text: text bbox_norm([x1,y1,x2,y2]归一化) fontsize fontcolor "
    "layout background_style start end；"
    "title_card: text w h duration bg fontsize layout（文字尾卡，无输入）；"
    "burn_subtitles: force_style，输入 video + subtitle(SRT)，"
    "以上均产物 video；\n"
    "probe: 无参（媒体元信息）；scene_detect: threshold（场景切换检测），"
    "二者返回 JSON 到 result，不产媒体产物。"
)

MEDIA_REF_DOC: Final = (
    "https URL / blobstore key（db:table:key）/ base64:xxx"
)


class _ContractModel(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)


class FfmpegStep(_ContractModel):
    op: Annotated[FfmpegOp, Field(description="白名单操作名")]
    params: dict[str, Any] = Field(
        default_factory=dict,
        description="该步 op 的参数对象",
    )


class FfmpegToolInput(_ContractModel):
    model_config = ConfigDict(
        extra="forbid",
        strict=True,
        str_strip_whitespace=True,
    )

    op: Annotated[
        FfmpegOp | None,
        Field(description=f"单操作名，与 pipeline 二选一。{OP_CATALOG}"),
    ] = None
    params: dict[str, Any] = Field(
        default_factory=dict,
        description="op 的参数对象，仅在 op 模式下生效",
    )
    pipeline: list[FfmpegStep] = Field(
        default_factory=list,
        description=(
            "多步操作，与 op 二选一。每步主产物按类型（video/audio/"
            "image）流入下一步的对应槽位，中间产物不落盘 blobstore。"
        ),
    )

    video: Annotated[
        str | None,
        Field(description=f"主视频输入：{MEDIA_REF_DOC}"),
    ] = None
    video2: Annotated[
        str | None,
        Field(description=f"第二视频输入（overlay_region/restore_audio）：{MEDIA_REF_DOC}"),
    ] = None
    audio: Annotated[
        str | None,
        Field(description=f"音频输入（mix_bgm/restore_audio 等）：{MEDIA_REF_DOC}"),
    ] = None
    image: Annotated[
        str | None,
        Field(description=f"图片输入（freeze/animate_image/overlay_region）：{MEDIA_REF_DOC}"),
    ] = None
    subtitle: Annotated[
        str | None,
        Field(description=f"SRT 字幕输入（burn_subtitles）：{MEDIA_REF_DOC}"),
    ] = None
    font: Annotated[
        str | None,
        Field(description=f"字体文件输入（文字类 op 可选）：{MEDIA_REF_DOC}"),
    ] = None
    videos: list[str] = Field(
        default_factory=list,
        description=f"视频列表输入（concat/concat_av），元素为 {MEDIA_REF_DOC}",
    )
    audios: list[str] = Field(
        default_factory=list,
        description=f"音频列表输入（mix_audio），元素为 {MEDIA_REF_DOC}",
    )

    @model_validator(mode="after")
    def _exactly_one_entry(self) -> "FfmpegToolInput":
        if (self.op is None) == (not self.pipeline):
            raise ValueError("op 与 pipeline 必须且只能提供一个")
        return self

    def op_summary(self) -> str:
        if self.op is not None:
            return self.op
        return "+".join(step.op for step in self.pipeline)


class InferActivityOutput(_ContractModel):
    """kess Infer 调用的原始结果（activity -> workflow）。"""

    kind: Literal["video", "audio", "image"] | None = None
    blobKey: str | None = None
    uri: str | None = None
    ssoUri: str | None = None
    outputSize: int | None = None
    result: dict[str, Any] | None = None


class FfmpegToolOutput(_ContractModel):
    artifacts: Annotated[
        list[Artifact],
        Field(description="媒体产物（0 或 1 个），uri 为可下载地址"),
    ]
    result: Annotated[
        dict[str, Any] | None,
        Field(description="分析类 op（probe/scene_detect）的 JSON 结果"),
    ] = None
