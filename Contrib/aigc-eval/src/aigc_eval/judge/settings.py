"""判定链的开关与阈值，全部由环境变量驱动。

集中在一处是为了让「这套判定当前是什么形态」一眼可查：4b 送几帧、4c 何时触发、
降载到多少边长。散落在编排里时，想回答「为什么这次只送了 9 帧」得翻遍几千行。

改这里等于改判定行为，但**不会**让已有缓存失效——缓存键只含 provider/mode/use_vlm
和 CACHE_VERSION。调阈值做对比实验时，要么带 refresh=True，要么手动抬 CACHE_VERSION。

当前协议是 native8-v1：4b 一次原生八维主判 + 4c 增量取证，没有旧的分维度扫描器，
也没有回退路径（4b 失败即报错，交由批处理重试）。旧五维加权体系的全部阈值
（崩坏投票、文字分块、一致性区域比对……）已随代码一并删除。
"""
from __future__ import annotations

import os


def _flag(name: str, default: str = "1") -> bool:
    return os.environ.get(name, default) not in {"0", "false", "False"}


# ---------------------------------------------------------------------------
# 数据集布局
# ---------------------------------------------------------------------------
# 仓库上一级里每个这些前缀开头的目录是一个案例集合。V2VA 是视频转视频，
# 它的 case 目录里额外有一条 reference_video 作为「被编辑的源视频」。
COLLECTION_PREFIXES = ("I2VA", "T2VA", "V2VA")

# 少数 case 的人工标注：展示用的短标签、关注点，以及离线替身要用的 profile 键。
# 只覆盖手工挑过的几条，未收录的 case 由 `_collection_focus` 按集合前缀兜底。
CURATED_CASES: dict[str, dict[str, str]] = {
    "Aio_i2v_v5-5s-115_0122": {
        "label": "网球旋转连续性",
        "focus": "旋转方向、纹理相位与运动连续性",
        "profile": "tennis_rotation",
    },
    "20260121_I2VA_混合_29_0121": {
        "label": "跑车空间关系",
        "focus": "相对位置、朝向与动作目标绑定",
        "profile": "car_spatial_relation",
    },
}


# ---------------------------------------------------------------------------
# 视频投递方式
# ---------------------------------------------------------------------------
# 默认把整段视频交给模型（原生视频理解）。超限时改为「按运动量加密抽取的关键帧
# + 全片缩略拼图」。
SEND_WHOLE_VIDEO = _flag("GEMINI_SEND_WHOLE_VIDEO")


# 体积上限只对「把视频 base64 塞进请求体」的路径有意义。
#
# 注意各通道的真实天花板不同，这个值只是**本地的保守闸门**：
#   · agentk 通道：平台侧 maxTotalBytes = 100MB，且全部素材共享该预算
#     （agentk/internal/capability/mediaunderstand/mediaunderstand.go）。
#     512 在那条通道上永远达不到，真正的拦截发生在服务端。
VIDEO_MAX_MB = float(os.environ.get("GEMINI_VIDEO_MAX_MB", "512"))


# ---------------------------------------------------------------------------
# 4b 原生八维主判
#
# 一次调用按固定 8 维、28 个子判据输出事实草案。输入是整段视频 + 若干张带镜头号
# 的高清帧。**没有分维度回退路径**：4b 失败直接抛错，由批处理按 --retries 重试，
# 而不是静默切换到另一套判据（那会让同一批数据里混进两种口径的结论）。
# ---------------------------------------------------------------------------
UNIFIED_SCAN_FRAMES = int(os.environ.get("JUDGE_UNIFIED_SCAN_FRAMES", "16"))


# 文字要判字形、崩坏要判结构，都是像素级判断，所以按最高的那个维度给分辨率。
UNIFIED_SCAN_MAX_SIDE = int(os.environ.get("JUDGE_UNIFIED_SCAN_MAX_SIDE", "1280"))


# 4b 是否附带整段视频。时序类判据（崩坏前后突变、运镜是否真的发生、音画同步）
# 靠它，只给静帧判不出来。
UNIFIED_SCAN_VIDEO = _flag("JUDGE_UNIFIED_SCAN_VIDEO")


# 观察器最多提供几个低置信区间供 4b 加密取证。这个信号只调度取证，**不是问题结论**
# ——观察器说「这里看不清」不等于「这里有问题」。
FOCUS_INTERVAL_LIMIT = 3


# 每个 focus 区间除中心帧外，前后各补一帧的时间偏移（秒）。
FOCUS_CONTEXT_SECONDS = 0.4


# 无论 focus 区间占掉多少预算，始终保留这么多张全片覆盖帧。
# 少了这条兜底，一个开场的低置信区间就能把整个帧预算吃光，
# 模型于是只看到视频开头却对全片下结论。
FOCUS_MIN_GLOBAL_FRAMES = 7


# ---------------------------------------------------------------------------
# 4c 增量取证
#
# 只对 4b 的 not_observable、观察器的 focus_intervals、指令 checklist 的 unknown
# 触发复核。4b 已明确的 violated/pass **不自动复核** —— 复核一切等于把成本翻倍
# 却只换来对已定论结果的反复确认。
# ---------------------------------------------------------------------------
TARGETED_RECHECK = _flag("JUDGE_TARGETED_RECHECK")


# 4c 最多附带几张候选高清帧（复用 4b 已抽好的帧，不重新抽）。
RECHECK_MAX_FRAMES = int(os.environ.get("JUDGE_RECHECK_MAX_FRAMES", "12"))


RECHECK_MAX_SIDE = int(os.environ.get("JUDGE_RECHECK_MAX_SIDE", "1280"))


# 音频独立复听。默认**开**，但只在视频真有音轨时才发起（判据见 pipeline 的调用点）。
#
# 曾经默认关，理由是"多数 case 无音轨或音频不是关注点，开着等于每条都多花一次调用"。
# 那个理由在 2026-09-01 之前还有一层没写出来的前提：音频判据本来就判不了，这次调用
# 买不到什么。现在买得到了——平台放开了音频入口，而 audio_model（flash-lite）实测能
# 稳定拿到音频本体。关着的代价变成"三个子判据永远停在未核实"，比多一次调用贵。
#
# "无音轨就不发起"这一条把原来的成本担忧消掉了大半：没声音的 case 一次也不多花。
AUDIO_RECHECK = _flag("JUDGE_AUDIO_RECHECK", "1")


# ---------------------------------------------------------------------------
# 问题定位（grounding）
#
# 把已定论的问题框到画面上，喂给报告页的视频组件。跑在定档**之后**，只补坐标、
# 不参与判定。
#
# 这里没有「哪些判据不可定位」的静态名单：native8 下同一个子判据既可能挂着
# 可定位的问题（画面里某个物体崩了），也可能挂着不可定位的（整体曝光偏了）。
# 改为逐条动态校验——bbox 必须落在模型自己声明为 problem/representative 的帧上，
# 否则标 locatable=False 并禁止画框。
# ---------------------------------------------------------------------------
GROUNDING_PASS = _flag("JUDGE_GROUNDING_PASS")


GROUNDING_MAX_FRAMES = int(os.environ.get("JUDGE_GROUNDING_MAX_FRAMES", "8"))


GROUNDING_MAX_SIDE = int(os.environ.get("JUDGE_GROUNDING_MAX_SIDE", "1280"))


# 整个证据阶段（含定位与证据帧渲染）的总开关。批量跑只要 JSON 时可以关掉。
SKIP_EVIDENCE = _flag("JUDGE_SKIP_EVIDENCE", "0")


# ---------------------------------------------------------------------------
# 视频部件的采样参数
#
# fps 与单帧 token 预算是一对权衡：高 fps 换时序密度，高单帧 token 换细节可辨。
# native8 把它们分成两档 —— detail 用于 4b（要判字形与结构），
# temporal 用于观察器与 4c（要判「什么时候变了」）。
# ---------------------------------------------------------------------------
VIDEO_FPS_DETAIL = float(os.environ.get("JUDGE_VIDEO_FPS_DETAIL", "2.0"))


VIDEO_FPS_TEMPORAL = float(os.environ.get("JUDGE_VIDEO_FPS_TEMPORAL", "4.0"))


VIDEO_FRAME_TOKENS_DETAIL = int(os.environ.get("JUDGE_VIDEO_FRAME_TOKENS_DETAIL", "1600"))


VIDEO_FRAME_TOKENS_TEMPORAL = int(os.environ.get("JUDGE_VIDEO_FRAME_TOKENS_TEMPORAL", "640"))


VIDEO_TOKENS_TOTAL = int(os.environ.get("JUDGE_VIDEO_TOKENS_TOTAL", "16000"))


# ---------------------------------------------------------------------------
# 参考素材分阶段预算
#
# 数据集里单条最多 9 图 + 2 音频 + 1 参考视频，全量塞进每个阶段会撑爆上下文；
# 但音频/参考视频**只有观察器阶段能感知**（后续阶段拿不到媒体），所以它们
# 不参与裁剪，只在观察器出现一次。图片按阶段重要性给不同额度。
# ---------------------------------------------------------------------------
REFERENCE_BUDGET: dict[str, dict[str, int]] = {
    "compile": {"image": 8, "audio": 0, "video": 0},
    "observe": {"image": 8, "audio": 4, "video": 2},
    "recheck": {"image": 4, "audio": 0, "video": 0},
    "consistency": {"image": 6, "audio": 0, "video": 0},
}


REFERENCE_MAX_SIDE = int(os.environ.get("JUDGE_REFERENCE_MAX_SIDE", "1024"))


# ---------------------------------------------------------------------------
# 素材件数预算的通道协商
#
# 各 provider 对「单次调用能带几件素材」的上限不同，而 4b 的提示词里会写明
# 「高清帧共 N 张，请对每一张都写一行，一张都不能漏」。如果 N 是按本地配置算的、
# 而传输层事后又把素材裁掉，提示词与实际载荷就会自相矛盾 —— 模型被要求为
# 不存在的帧写记录，4b 的逐帧校验随之失真。
#
# 所以帧数必须在**构造提示词之前**就按通道上限敲定。provider 通过
# ``media_capability()`` 声明自己的上限，这里做协商；不声明则视为无上限。
#
# 相关平台约束（截至接入时）：
#   agentk.understand_media 的 mediaUrls 是 maxItems=10
#   （agentk/internal/capability/mediaunderstand/understand_media.schema.json）。
# ---------------------------------------------------------------------------
def plan_frame_budget(
    *,
    requested_frames: int,
    reserved_media: int,
    max_media: int | None,
) -> tuple[int, dict[str, int] | None]:
    """按通道上限敲定 4b 实际可送的高清帧数。

    ``reserved_media`` 是本次调用中非帧素材的件数（整段视频、V2V 源视频、参考图），
    它们信息密度最高，优先保留。返回 ``(帧数, 降载说明或 None)``。

    帧数被压到 0 时不静默通过：调用方应据此判断这条通道是否还能承载 4b。
    """
    if max_media is None or max_media <= 0:
        return requested_frames, None
    allowed = max_media - reserved_media
    if allowed >= requested_frames:
        return requested_frames, None
    granted = max(0, allowed)
    return granted, {
        "cap": max_media,
        "reserved": reserved_media,
        "requested_frames": requested_frames,
        "granted_frames": granted,
    }


# ---------------------------------------------------------------------------
# 成本
# ---------------------------------------------------------------------------
# 模型计费单价（每百万 tokens）；思考 token 计入输出价。
# agentk 通道拿不到 usage（CapabilityOutput 只有 text/model/params），
# 那条通道上的成本统计会是 0 并带 tokens_unavailable 标记 —— 是缺失，不是免费。
PRICE_INPUT_PER_M = 11.97


PRICE_OUTPUT_PER_M = 71.79


# ---------------------------------------------------------------------------
# 缓存
# ---------------------------------------------------------------------------
# 报告 schema 变化时必须抬版本，否则旧缓存会以缺字段的形态被渲染。
# v76 起只接受 native8-v1；v41 及更早的五维报告一律视为未命中。
CACHE_VERSION = "v76"
