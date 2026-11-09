# Storyboard Film Example Contract

This repository contains two interchangeable Temporal implementations of the
same agent-k capability.

## Workflow

- Type: `StoryboardFilmExample`
- Go capability ID: `contrib.storyboard_example.storyboard_film_go`
- Python capability ID: `contrib.storyboard_example.storyboard_film_python`
- Profile pipeline name: `storyboard_film_example`
- Go task queue: `contrib.storyboard-example-go`
- Python task queue: `contrib.storyboard-example-py`

Each Worker publishes one workflow Capability with a blocking Tool policy. The
Worker declaration has no separate Tool or Pipeline name: the Capability ID is
its unique public identity, while the Temporal Workflow Type remains an internal
route. A Session Profile binds the Capability ID to the pipeline name above.

Input:

```json
{
  "topic": "雨夜的猫",
  "shots": 3,
  "chatModel": "chat-model-ref",
  "imageModel": "image-model-ref",
  "videoModel": "video-model-ref"
}
```

`topic`, `chatModel`, `imageModel`, and `videoModel` are required. `shots`
defaults to 3 and must be between 1 and 8.

Output:

```json
{
  "artifacts": [
    {
      "type": "video",
      "uri": "https://example.invalid/film.mp4"
    }
  ],
  "clipCount": 3
}
```

## Core capability ABI

The workflow invokes these registered capabilities through the AgentK Worker SDK:

### `agentk.generate_text`

```json
{
  "model": "chat-model-ref",
  "prompt": "string",
  "outputSchema": {}
}
```

Returns:

```json
{
  "result": {
    "shots": [
      {
        "frame": "static frame prompt",
        "motion": "motion prompt"
      }
    ]
  }
}
```

### `agentk.generate_image`

```json
{
  "model": "image-model-ref",
  "prompt": "static frame prompt",
  "aspectRatio": "16:9"
}
```

Returns at least:

```json
{
  "artifacts": [
    {
      "type": "image",
      "uri": "https://example.invalid/frame.png",
      "model": "image-model-ref"
    }
  ]
}
```

### `agentk.generate_video`

```json
{
  "model": "video-model-ref",
  "firstFrame": "https://example.invalid/frame.png",
  "prompt": "motion prompt"
}
```

Returns:

```json
{
  "artifacts": [
    {
      "type": "video",
      "uri": "https://example.invalid/clip.mp4",
      "model": "video-model-ref"
    }
  ]
}
```

### `agentk.compose_video`

```json
{
  "videoUrls": [
    "https://example.invalid/clip-0.mp4",
    "https://example.invalid/clip-1.mp4"
  ]
}
```

Returns:

```json
{
  "artifacts": [
    {
      "type": "video",
      "uri": "https://example.invalid/film.mp4"
    }
  ],
  "clipCount": 2
}
```

## Execution semantics

- A shot generates its frame before generating its video.
- Different shots run concurrently.
- If the model returns more shots than requested, the workflow keeps only the
  requested prefix before starting paid image and video work.
- Failed shots are discarded while successful shots retain source order.
- Composition starts when at least one shot succeeds.
- The workflow fails without calling composition when every shot fails.
- Core capability payloads contain only business input; agent-k session, tenant,
  and execution metadata are carried outside the business ABI.

Both implementations use the official agent-k Worker SDK to report `board` and
`clip/N` progress. Pipeline Run responses and Event Log events retain both the
Profile pipeline name and the resolved `capabilityId`.
