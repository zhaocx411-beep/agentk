from typing import Annotated, Any, Final

from agentk.worker import Artifact
from pydantic import BaseModel, ConfigDict, Field

CAPABILITY_ID: Final = "contrib.storyboard_example.storyboard_film_python"
WORKFLOW_TYPE: Final = "StoryboardFilmExample"
WORKER_TASK_QUEUE: Final = "contrib.storyboard-example-py"
DEFAULT_SHOTS: Final = 3
MAX_SHOTS: Final = 8

GENERATE_TEXT_CAPABILITY: Final = "agentk.generate_text"
GENERATE_IMAGE_CAPABILITY: Final = "agentk.generate_image"
GENERATE_VIDEO_CAPABILITY: Final = "agentk.generate_video"
COMPOSE_VIDEO_CAPABILITY: Final = "agentk.compose_video"


class _ContractModel(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)


class StoryboardInput(_ContractModel):
    model_config = ConfigDict(
        extra="forbid",
        strict=True,
        str_strip_whitespace=True,
    )

    topic: Annotated[str, Field(min_length=1, description="短片主题")]
    chatModel: Annotated[
        str,
        Field(min_length=1, description="分镜文案模型"),
    ]
    imageModel: Annotated[
        str,
        Field(min_length=1, description="首帧图模型"),
    ]
    videoModel: Annotated[
        str,
        Field(min_length=1, description="视频片段模型"),
    ]
    shots: Annotated[
        int,
        Field(ge=1, le=MAX_SHOTS, description="镜头数，缺省 3"),
    ] = DEFAULT_SHOTS


class StoryboardOutput(_ContractModel):
    artifacts: list[Artifact]
    clipCount: int


class StoryboardShot(_ContractModel):
    frame: str
    motion: str


class StoryboardBoard(_ContractModel):
    shots: list[StoryboardShot]


class GenerateTextInput(_ContractModel):
    model: str
    prompt: str
    outputSchema: dict[str, Any]


class GenerateTextOutput(_ContractModel):
    result: StoryboardBoard


class GenerateImageInput(_ContractModel):
    model: str
    prompt: str
    aspectRatio: str


class GenerateImageOutput(_ContractModel):
    artifacts: list[Artifact]


class GenerateVideoInput(_ContractModel):
    model: str
    firstFrame: str
    prompt: str


class GenerateVideoOutput(_ContractModel):
    artifacts: list[Artifact]


class ConcatVideosInput(_ContractModel):
    videoUrls: list[str]


class ConcatVideosOutput(_ContractModel):
    artifacts: list[Artifact]
    clipCount: int
