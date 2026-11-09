# AgentCut render semantics

`timeline.Doc` remains schema version 1. M3A fields are optional and old
documents continue to decode with their historical defaults.

## Item audio and effects

```json
{
  "volume": 0,
  "muted": false,
  "fadeInFrames": 0,
  "fadeOutFrames": 0
}
```

- Item and Track `volume` are gains in `[0, 2]`; zero is the legacy unset
  value and therefore means `1`.
- Silence is represented only by `muted`.
- Effective gain is zero when either the item or its track is muted,
  otherwise it is `effectiveTrackVolume * effectiveItemVolume`.
- Fades are supported only by video/audio items. Their sum cannot exceed the
  Timeline duration of the item.
- Video fades affect both the picture and embedded audio. Audio items use an
  audio fade.

## Item transform

```json
{
  "transform": {
    "x": 0.6,
    "y": 0.05,
    "width": 0.35,
    "height": 0.35,
    "opacity": 0.8,
    "fit": "contain"
  }
}
```

Coordinates and dimensions are normalized to the output canvas. Width and
height are positive; the rectangle must remain inside the canvas. `fit` is
`contain` or `cover`. Opacity zero is the backward-compatible unset value and
renders as one. A missing transform means full-canvas `contain`.

Normalized geometry is converted to integer pixels deterministically: left/top
use floor, right/bottom use ceil, and the result is clamped to the canvas.
This keeps every valid positive normalized rectangle at least one pixel large.

## Executor support

Local ffmpeg rendering supports M3A audio speed, embedded-video gain/mute,
audio/video fades, transforms, opacity, fit, and canvas background color.

The Agent-K ffmpeg capability continues to support its existing operation
catalog. A document requiring an M3A operation that is not represented by
that catalog fails before execution with `UNSUPPORTED_RENDER_FEATURE`; it is
never rendered with silently degraded semantics. Deployments that need these
features must select the Local executor.

Render cache identity is versioned independently from Timeline JSON. M3A uses
`agentcut-render-v3`, so no v2 artifact can be reused.
