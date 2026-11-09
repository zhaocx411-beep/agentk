const test = require("node:test");
const assert = require("node:assert/strict");

const {
  defaultsFor,
  exampleFor,
  mergeUploadedUrls,
  selectOptionsFor,
  uploadAcceptFor,
} = require("../src/agentk_capability_demo/static/form-defaults.js");

test("schema defaults and enums take precedence over generated examples", () => {
  assert.equal(exampleFor({ type: "string", default: "balanced" }, {}), "balanced");
  assert.equal(exampleFor({ type: "string", enum: ["fast", "quality"] }, {}), "fast");
});

test("provider voice options render as labeled selects and drive required defaults", () => {
  const schema = {
    type: "string",
    "x-options": [
      {value: "Cherry", label: "芊晓（Cherry）"},
      {value: "Ethan", label: "晨煦（Ethan）"},
    ],
  };
  assert.deepEqual(selectOptionsFor(schema), [
    {value: "Cherry", label: "芊晓（Cherry）"},
    {value: "Ethan", label: "晨煦（Ethan）"},
  ]);
  assert.equal(exampleFor(schema, {name: "voiceId"}), "Cherry");
  assert.equal(exampleFor({type: "string", "x-options": []}, {name: "voiceId"}), undefined);
});

test("ordinary enums continue to expose simple select options", () => {
  assert.deepEqual(selectOptionsFor({enum: ["fast", "quality"]}), [
    {value: "fast", label: "fast"},
    {value: "quality", label: "quality"},
  ]);
  assert.equal(selectOptionsFor({type: "string"}), null);
});

test("only required fields without defaults receive generated examples", () => {
  const schema = {
    type: "object",
    required: ["prompt", "count"],
    properties: {
      prompt: { type: "string" },
      count: { type: "integer", minimum: 1 },
      optionalNote: { type: "string" },
      enabled: { type: "boolean", default: true },
    },
  };

  assert.deepEqual(defaultsFor(schema, "agentk.generate_image"), {
    prompt: "一只橘猫坐在未来感工作台前，电影级光影，细节清晰",
    count: 1,
    enabled: true,
  });
});

test("media fields and required arrays get editable URL examples", () => {
  const schema = {
    type: "object",
    required: ["videoUrl", "referenceImages"],
    properties: {
      videoUrl: { type: "string" },
      referenceImages: { type: "array", items: { type: "string" } },
    },
  };

  assert.deepEqual(defaultsFor(schema, "agentk.edit_image"), {
    videoUrl: "https://example.com/input.mp4",
    referenceImages: ["https://example.com/reference.png"],
  });
});

test("Keffects capabilities receive runnable schema-driven examples", () => {
  assert.deepEqual(
    defaultsFor(
      {type: "object", required: ["audio"], properties: {audio: {type: "string"}}},
      "agentk.create_voice_embedding",
    ),
    {audio: "https://example.com/input.mp3"},
  );
  assert.deepEqual(
    defaultsFor(
      {type: "object", required: ["video"], properties: {video: {type: "string"}}},
      "agentk.identify_faces",
    ),
    {video: "https://example.com/input.mp4"},
  );

  const createSubject = {
    type: "object",
    required: ["name"],
    properties: {
      name: {type: "string", minLength: 1, maxLength: 20},
      images: {type: "array", minItems: 1, maxItems: 3, items: {type: "string"}},
      video: {type: "string"},
      voiceEmbeddings: {type: "array", maxItems: 1, items: {type: "string"}},
    },
    oneOf: [
      {required: ["images"], not: {required: ["video"]}},
      {required: ["video"], not: {required: ["images"]}},
    ],
  };
  assert.deepEqual(defaultsFor(createSubject, "agentk.create_kling_subject"), {
    name: "测试主体",
    images: ["https://example.com/reference.png"],
  });

  const lipSync = {
    type: "object",
    required: ["video", "audio", "faceIndexKey", "faceId", "soundEndTime"],
    properties: {
      video: {type: "string"},
      audio: {type: "string"},
      faceIndexKey: {type: "string"},
      faceId: {type: "string"},
      soundStartTime: {type: "integer", default: 0},
      soundEndTime: {type: "integer", minimum: 1},
    },
  };
  assert.deepEqual(defaultsFor(lipSync, "agentk.lip_sync_video"), {
    video: "https://example.com/input.mp4",
    audio: "https://example.com/input.mp3",
    faceIndexKey: "face-index-key-from-identify-faces",
    faceId: "face-id-from-identify-faces",
    soundStartTime: 0,
    soundEndTime: 3000,
  });
});

test("kling omni examples satisfy the first supported material branch", () => {
  const schema = {
    type: "object",
    required: ["model", "prompt"],
    properties: {
      model: {type: "string", enum: ["kling-3.0-omni"]},
      prompt: {type: "string"},
      referImages: {type: "array", minItems: 1, items: {type: "string"}},
      featureVideo: {type: "string"},
      subjectLists: {type: "array", minItems: 1, items: {type: "string"}},
    },
    anyOf: [
      {required: ["referImages"]},
      {required: ["featureVideo"]},
      {required: ["subjectLists"]},
    ],
  };

  assert.deepEqual(defaultsFor(schema, "agentk.kling_omni_video"), {
    model: "kling-3.0-omni",
    prompt: "参考 @image_1 的主体和风格，生成一段平稳推进的电影感镜头",
    referImages: ["https://example.com/reference.png"],
  });
  assert.deepEqual(
    exampleFor(schema.properties.subjectLists, {
      capabilityId: "agentk.kling_omni_video",
      name: "subjectLists",
    }),
    ["subject-list-from-create-kling-subject"],
  );
});

test("updated text and video capabilities expose their new parameters", () => {
  const textSchema = {
    type: "object",
    additionalProperties: false,
    required: ["model", "prompt"],
    properties: {
      model: {type: "string", minLength: 1},
      prompt: {type: "string", minLength: 1},
      outputSchema: {type: "object"},
    },
  };
  assert.deepEqual(defaultsFor(textSchema, "agentk.generate_text"), {
    model: "deepseek-v4-pro",
    prompt: "用三点总结 AgentK 的核心能力，每点不超过 30 字",
  });
  assert.deepEqual(exampleFor(textSchema.properties.outputSchema, {
    capabilityId: "agentk.generate_text",
    name: "outputSchema",
  }), {});

  const videoSchema = {
    type: "object",
    additionalProperties: false,
    required: ["model", "firstFrame", "prompt"],
    properties: {
      model: {type: "string", minLength: 1},
      firstFrame: {type: "string", minLength: 1},
      lastFrame: {type: "string"},
      prompt: {type: "string", minLength: 1},
      resolution: {type: "string", enum: ["720p", "1080p", "4k"], default: "1080p"},
      duration: {type: "string", pattern: "^(?:[4-9]|1[0-5])$", default: "5"},
      audio: {type: "string", enum: ["native", "off"], default: "off"},
    },
  };
  assert.deepEqual(defaultsFor(videoSchema, "agentk.generate_video"), {
    model: "kling-3.0",
    firstFrame: "https://example.com/reference.png",
    prompt: "主体缓慢转身看向镜头，镜头平稳推进，环境声自然",
    resolution: "1080p",
    duration: "5",
    audio: "off",
  });
});

test("oneOf object arrays generate a valid first operation", () => {
  const schema = {
    type: "object",
    required: ["operations"],
    properties: {
      operations: {
        type: "array",
        items: {
          oneOf: [
            {
              type: "object",
              required: ["type", "startSeconds", "durationSeconds"],
              properties: {
                type: { type: "string", enum: ["trim"] },
                startSeconds: { type: "number", minimum: 0 },
                durationSeconds: { type: "number", exclusiveMinimum: 0 },
              },
            },
          ],
        },
      },
    },
  };

  assert.deepEqual(defaultsFor(schema, "agentk.process_video"), {
    operations: [{ type: "trim", startSeconds: 0, durationSeconds: 5 }],
  });
});

test("capability-specific deployment and prompt examples are understandable", () => {
  assert.equal(
    exampleFor({ type: "string" }, { capabilityId: "agentk.detect_shots", name: "model" }),
    "shots-maas-prod",
  );
  assert.equal(
    exampleFor({ type: "string" }, { capabilityId: "agentk.generate_text", name: "model" }),
    "deepseek-v4-pro",
  );
  assert.match(
    exampleFor({ type: "string" }, { capabilityId: "agentk.web_search", name: "query" }),
    /AgentK/,
  );
  assert.equal(
    exampleFor(
      { type: "string", format: "uri" },
      { capabilityId: "agentk.fetch_product_detail", name: "productUrl" },
    ),
    "https://item.taobao.com/item.htm?id=1069836522906",
  );
});

test("frame fields support image upload while still accepting a URL", () => {
  assert.equal(uploadAcceptFor("firstFrame", { type: "string" }), "image/*");
  assert.equal(uploadAcceptFor("lastFrame", { type: "string" }), "image/*");
  assert.equal(uploadAcceptFor("referenceImage", { type: "string" }), "image/*");
  assert.equal(uploadAcceptFor("videoUrl", { type: "string" }), "video/*");
  assert.equal(uploadAcceptFor("audioUrl", { type: "string" }), "audio/*");
  assert.equal(uploadAcceptFor("video", { type: "string" }), "video/*");
  assert.equal(uploadAcceptFor("audio", { type: "string" }), "audio/*");
  assert.equal(uploadAcceptFor("imageSize", { type: "string" }), null);
  assert.equal(uploadAcceptFor("title", { type: "string" }), null);
});

test("media URL arrays support typed multi-file uploads", () => {
  const stringArray = { type: "array", items: { type: "string" } };
  assert.equal(uploadAcceptFor("referenceImages", stringArray), "image/*");
  assert.equal(uploadAcceptFor("videoUrls", stringArray), "video/*");
  assert.equal(uploadAcceptFor("audioUrls", stringArray), "audio/*");
  assert.equal(uploadAcceptFor("mediaUrls", stringArray), "image/*,video/*,audio/*");
  assert.equal(uploadAcceptFor("referenceVideos", stringArray), "video/*");
  assert.equal(uploadAcceptFor("referenceAudios", stringArray), "audio/*");
  assert.equal(uploadAcceptFor("featureVideo", { type: "string" }), "video/*");
  assert.equal(uploadAcceptFor("subtitleUrl", { type: "string" }), ".srt,.ass,.vtt");
  assert.equal(uploadAcceptFor("subtitleUrls", stringArray), ".srt,.ass,.vtt");
  assert.equal(uploadAcceptFor("fileUrls", stringArray), "*/*");
  assert.equal(uploadAcceptFor("documentUrl", { type: "string" }), "*/*");
  assert.equal(
    exampleFor({ type: "string" }, { capabilityId: "agentk.compose_video", name: "subtitleUrl" }),
    "https://example.com/subtitles.srt",
  );
});

test("structured arrays are not mistaken for direct file inputs", () => {
  const objectArray = { type: "array", items: { type: "object" } };
  assert.equal(uploadAcceptFor("audioTracks", objectArray), null);
  assert.equal(uploadAcceptFor("subtitleSegments", objectArray), null);
  assert.equal(uploadAcceptFor("operations", objectArray), null);
  assert.equal(uploadAcceptFor("audio", { type: "string", enum: ["off", "native"] }), null);
  assert.equal(uploadAcceptFor("voiceEmbeddings", { type: "array", items: {type: "string"} }), null);
  assert.equal(uploadAcceptFor("subjectLists", { type: "array", items: {type: "string"} }), null);
  assert.equal(uploadAcceptFor("video_prompt", { type: "string" }), null);
  assert.equal(uploadAcceptFor("subtitleFormat", { type: "string", enum: ["srt"] }), null);
});

test("web and product URLs do not expose file upload controls", () => {
  assert.equal(uploadAcceptFor("productUrl", { type: "string", format: "uri" }), null);
  assert.equal(uploadAcceptFor("url", { type: "string", format: "uri" }), null);
  assert.equal(uploadAcceptFor("websiteUri", { type: "string" }), null);
  assert.equal(uploadAcceptFor("callbackUrl", { type: "string" }), null);
});

test("multi-upload replaces generated example URLs and preserves real URLs", () => {
  assert.deepEqual(
    mergeUploadedUrls(
      ["https://example.com/reference.png"],
      ["http://127.0.0.1:8080/uploads/one.png", "http://127.0.0.1:8080/uploads/two.png"],
      true,
    ),
    ["http://127.0.0.1:8080/uploads/one.png", "http://127.0.0.1:8080/uploads/two.png"],
  );
  assert.deepEqual(
    mergeUploadedUrls(
      ["https://assets.test/existing.png"],
      ["https://assets.test/new.png"],
      true,
    ),
    ["https://assets.test/existing.png", "https://assets.test/new.png"],
  );
});
