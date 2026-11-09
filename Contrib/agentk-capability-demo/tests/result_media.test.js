const test = require("node:test");
const assert = require("node:assert/strict");

const {
  mediaArtifactsFor,
  mediaKindFor,
} = require("../src/agentk_capability_demo/static/result-media.js");

test("extracts and deduplicates typed image, video, audio, and file artifacts", () => {
  const output = {
    artifacts: [
      { type: "image", uri: "https://assets.test/frame.jpg", description: "frame" },
      { type: "video", uri: "https://assets.test/movie", description: "movie" },
      { type: "audio", uri: "https://assets.test/voice.wav" },
      { type: "document", uri: "https://assets.test/subtitles.srt" },
      { type: "image", uri: "https://assets.test/frame.jpg" },
    ],
  };

  assert.deepEqual(mediaArtifactsFor(output), [
    { kind: "image", uri: "https://assets.test/frame.jpg", description: "frame", type: "image" },
    { kind: "video", uri: "https://assets.test/movie", description: "movie", type: "video" },
    { kind: "audio", uri: "https://assets.test/voice.wav", description: "", type: "audio" },
    { kind: "file", uri: "https://assets.test/subtitles.srt", description: "", type: "document" },
  ]);
});

test("recognizes common media URL fields and nested output objects", () => {
  const output = {
    preview: { imageUrl: "https://assets.test/generated?token=1" },
    videoUrl: "https://assets.test/generated.mp4",
    audioUrl: "https://assets.test/generated.mp3",
  };
  assert.deepEqual(
    mediaArtifactsFor(output).map(item => [item.kind, item.uri]),
    [
      ["image", "https://assets.test/generated?token=1"],
      ["video", "https://assets.test/generated.mp4"],
      ["audio", "https://assets.test/generated.mp3"],
    ],
  );
});

test("does not render ordinary web-search result links as artifacts", () => {
  const output = {
    results: [{ title: "AgentK", url: "https://example.com/page", summary: "result" }],
  };
  assert.deepEqual(mediaArtifactsFor(output), []);
});

test("does not mistake image size and media option strings for URLs", () => {
  const output = {
    artifacts: [{
      type: "image",
      uri: "https://assets.test/generated.png",
      params: { imageSize: "2K", aspectRatio: "16:9", format: "png" },
    }],
  };
  assert.deepEqual(
    mediaArtifactsFor(output).map(item => item.uri),
    ["https://assets.test/generated.png"],
  );
});

test("infers media type from mime, explicit type, field name, and extension", () => {
  assert.equal(mediaKindFor({ mimeType: "image/png", uri: "https://assets.test/blob" }), "image");
  assert.equal(mediaKindFor({ type: "video", uri: "https://assets.test/blob" }), "video");
  assert.equal(mediaKindFor({ uri: "https://assets.test/blob" }, "speakerAudioUrl"), "audio");
  assert.equal(mediaKindFor({ uri: "https://assets.test/file.webm?x=1" }), "video");
  assert.equal(mediaKindFor({ uri: "https://assets.test/file.pdf" }), "file");
});
