"""Runtime configuration for Video Judge.

凭据只从环境变量读。此前还支持从 ``../gemini.md`` 里扒 curl 示例的请求头，那是
Gemini 直连通道时期为本地 demo 开的方便门；通道下线后一并删掉——配置来源只留一处，
排查"到底读到了哪份凭据"时不用再翻两个地方。
"""

from __future__ import annotations

import os
from dataclasses import dataclass

from aigc_eval import paths


PROJECT_ROOT = paths.WORKSPACE_ROOT
DEMO_ROOT = paths.REPO_ROOT


# 可用的模型通道。曾经有 gemini（直连 Vertex/KLink）与 keye（本地 OpenAI 兼容），
# 都已下线：判定早已固定走 agentk，而 understand_media 底层就是 Gemini，直连拿不到
# 不同的模型，只多一套要自己维护的凭据与重试。
# 这是唯一真源——健康检查、报告渲染、前端下拉、批处理参数校验都读它。
LIVE_PROVIDERS = ("agentk",)
DEFAULT_PROVIDER = os.environ.get("VIDEO_JUDGE_PROVIDER", "agentk")


def normalize_provider(value: str | None) -> str:
    provider = (value or DEFAULT_PROVIDER).strip().lower()
    if provider not in LIVE_PROVIDERS:
        raise ValueError(f"不支持的模型：{provider}")
    return provider


@dataclass(frozen=True)
class AgentKConfig:
    """走 AgentK 平台的媒体理解通道。

    这是唯一的通道。它不直连模型，而是把每次调用变成一次
    ``InvokeCapability`` 直调（``agentk.understand_media`` 或
    ``agentk.generate_text``）。选它的理由是统一额度、统一审计；
    代价见 README「AgentK 通道的能力边界」。
    """

    base_url: str
    # model 必须落在 understand_media.schema.json 的 enum 里，否则 Activity 直接拒绝。
    model: str
    # 质检流程里「拆解提示词」这类环节没有任何素材，而 understand_media 的
    # mediaUrls 是 minItems=1，走不通。这类调用改派给 agentk.generate_text，
    # 它的 model 走 ChatClient 查表，必须是 chat 家族——填多模态模型会被拒
    # （实测：gemini-3-flash-preview 报 text capability not available）。
    chat_model: str
    # 音频阶段单独用一个模型，不跟 ``model`` 走。
    #
    # 平台 2026-09-01 16:05 放开 mediaUrls 收音频，但两个模型的行为差得很远（实测见
    # README「音频」一节）：pro 有大约一半的调用悄悄退回"服务端转写成文字再注入提示词"
    # ——接口通、台词对、还带时间戳，只有音色/语调/音质/配乐一概拿不到，报告里看不出
    # 任何异常；flash-lite 则 8/8 都真拿到了音频。
    #
    # 所以这不是"挑个便宜模型省钱"，是音频判据要想成立就只能走 flash-lite。反过来主
    # 视觉判定继续留在 pro：flash-lite 对非语音声音事件不敏感（同样实测 0/8），把整条
    # 判定链换过去是拿视觉精度换音频精度。
    audio_model: str = "gemini-3.1-flash-lite"
    # 天琴服务名，AgentK 从 SDK 0.3.0 起的唯一鉴权机制（旧的 AGENTK_API_KEY
    # 平台已下线，SDK 也不再支持发 Bearer）。线上由部署平台注入，本地自己 export。
    # credential 一般不需要：网关按服务名授权，有值时才会附带。
    service_name: str = ""
    service_credential: str = ""
    timeout_seconds: float = 420.0
    # understand_media 的 mediaUrls 上限为 10（schema maxItems）。超出会被
    # 客户端按均匀采样降载，而不是整条 case 失败。
    max_media: int = 10
    # understand_media 没有 responseMimeType，只能靠提示词约束输出形状。
    enforce_json_hint: bool = True
    # 用哪条传输通道连网关：``http``（固定 endpoint）或 ``grpc``（KESS 发现）。
    #
    # 默认 http 是给显式传了 base_url 的场景（测试里的假服务器、本地联调），
    # 那种情况下必须按给定地址走。真实取值由 ``default_agentk_transport`` 推导，
    # 理由见那里。
    transport: str = "http"

    @property
    def configured(self) -> bool:
        return bool(
            self.base_url and self.model and self.chat_model and self.service_name
        )


# AgentK 的 HTTP 网关地址，与 SDK 0.3.0 的
# python/client/src/agentk/client/http/__init__.py 保持一致。
STAGING_GATEWAY = "https://agentk-api.staging.kuaishou.com"
PRODUCTION_GATEWAY = "http://agentk-api.internal"


def default_agentk_gateway(stage: str) -> str:
    """按部署环境选网关，规则照抄 SDK 的 _gateway_url。

    KWS_SERVICE_STAGE 由部署平台注入；本地不设就落到生产网关（内网域名），
    所以本地联调要么 export KWS_SERVICE_STAGE=STAGING，要么显式给
    AGENTK_BASE_URL——deploy/run_agentk.sh 两个都会设上。
    """
    if stage.strip().upper() in {"STAGING", "CANDIDATE"}:
        return STAGING_GATEWAY
    return PRODUCTION_GATEWAY


def default_agentk_transport(stage: str, explicit_base_url: str = "") -> str:
    """按部署环境选传输通道。

    **生产环境必须走 KESS gRPC**：那个 HTTP 固定网关（``agentk-api.internal``）
    在生产不对我们开放。实测是在 prod 容器里用同一份身份、同一个进程做的 A/B——
    HTTP 回 ``unauthorized``，KESS gRPC 正常回 ``display_name='AIGC Eval'``。
    文档里也写了这条（client/transports：生产服务使用 KESS gRPC h2c，非 KESS 的
    生产连接要平台单独确认接入地址）。

    这个坑之所以值得写这么长：两条通道失败时的错误一模一样，都是 ``unauthorized``，
    与"服务名没登记"无法区分。我们为此白等了平台侧一轮加白。

    staging 继续走 HTTP，不是偷懒：KESS 发现依赖运行环境的 kess-agent，开发机上
    未必有，而 HTTP 网关按 stage 推导就能连。所有本地联调和离线测试都靠这条。

    显式给了 ``AGENTK_BASE_URL`` 时一律 HTTP：那个变量的语义就是"按这个地址连"，
    测试里的假服务器也走它。
    """
    if explicit_base_url:
        return "http"
    if stage.strip().upper() in {"STAGING", "CANDIDATE"}:
        return "http"
    return "grpc"


UNDERSTAND_MEDIA_MODELS = (
    "gemini-3.1-pro-preview",
    "gemini-3-flash-preview",
    "gemini-3.1-flash-lite",
)


# 哪些阶段改用 ``audio_model``。键是 ``generate_json(stage=...)`` 传进来的阶段名。
#
# 用集合而不是「阶段 → 模型」的映射：音频是目前唯一有理由换模型的维度，写成通用映射
# 会邀请别人按喜好给各阶段配模型，而那会让同一份判据在不同 case 上由不同模型给出，
# 报告之间就不可比了。要再加一档时再改成映射，届时必须一起说明可比性怎么保。
#
# 4c 的复听与反证两次调用都传 stage="audio"；参考音频对照另有一个阶段。
AUDIO_STAGES = frozenset({"audio", "audio_reference"})


def load_agentk_config() -> AgentKConfig:
    stage = os.environ.get("KWS_SERVICE_STAGE", "")
    explicit_base_url = os.environ.get("AGENTK_BASE_URL", "")
    return AgentKConfig(
        # AGENTK_BASE_URL 等价于 SDK 的 Options.endpoint：显式指定就用它，
        # 否则按 KWS_SERVICE_STAGE 推导。走 grpc 时这个值只用于展示（真正的
        # 地址由 KESS 发现），但仍然保留，health 接口和日志靠它说明连的是哪套环境。
        base_url=(
            explicit_base_url or default_agentk_gateway(stage)
        ).rstrip("/"),
        transport=default_agentk_transport(stage, explicit_base_url),
        service_name=os.environ.get("KWS_SERVICE_NAME", ""),
        service_credential=os.environ.get("KWS_SERVICE_CREDENTIAL", ""),
        model=os.environ.get("AGENTK_UNDERSTAND_MODEL", "gemini-3.1-pro-preview"),
        chat_model=os.environ.get("AGENTK_CHAT_MODEL", "deepseek-v4-pro"),
        audio_model=os.environ.get("AGENTK_AUDIO_MODEL", "gemini-3.1-flash-lite"),
        timeout_seconds=float(os.environ.get("AGENTK_TIMEOUT", "420")),
        max_media=int(os.environ.get("AGENTK_MAX_MEDIA", "10")),
        enforce_json_hint=os.environ.get("AGENTK_JSON_HINT", "1") not in ("0", "false", "False"),
    )


