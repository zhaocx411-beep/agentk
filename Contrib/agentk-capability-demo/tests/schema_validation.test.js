const test = require("node:test");
const assert = require("node:assert/strict");

const { validateCapabilityArgs } = require("../src/agentk_capability_demo/static/schema-validation.js");

test("validates common JSON Schema constraints before submission", () => {
  const schema = {
    type: "object", additionalProperties: false, required: ["prompt", "duration", "images"],
    properties: {
      prompt: {type: "string", minLength: 3},
      duration: {type: "integer", minimum: 4, maximum: 15},
      images: {type: "array", minItems: 1, maxItems: 2, items: {type: "string", format: "uri"}},
    },
  };
  const errors = validateCapabilityArgs(schema, {prompt: "x", duration: 3, images: [], extra: true});
  assert.deepEqual(errors.map(error => error.path), ["prompt", "duration", "images", "extra"]);
});

test("validates anyOf required groups", () => {
  const schema = {type: "object", anyOf: [{required: ["referImages"]}, {required: ["featureVideo"]}], properties: {referImages: {type: "array"}, featureVideo: {type: "string"}}};
  assert.match(validateCapabilityArgs(schema, {})[0].message, /至少一组/);
  assert.deepEqual(validateCapabilityArgs(schema, {referImages: ["https://example.com/a.png"]}), []);
});

test("validates create subject exclusive image or video inputs", () => {
  const schema = {
    type: "object",
    required: ["name"],
    properties: {
      name: {type: "string"},
      images: {type: "array", minItems: 1, items: {type: "string"}},
      video: {type: "string"},
    },
    oneOf: [
      {required: ["images"], not: {required: ["video"]}},
      {required: ["video"], not: {required: ["images"]}},
    ],
  };
  assert.deepEqual(validateCapabilityArgs(schema, {name: "角色", images: ["https://example.com/a.png"]}), []);
  assert.deepEqual(validateCapabilityArgs(schema, {name: "角色", video: "https://example.com/a.mp4"}), []);
  assert.equal(validateCapabilityArgs(schema, {name: "角色"})[0].path, "");
  assert.equal(validateCapabilityArgs(schema, {name: "角色", images: ["a"], video: "b"})[0].path, "");
});

test("supports JSON Schema not constraints used by capability manifests", () => {
  const schema = {type: "object", not: {required: ["forbidden"]}};
  assert.deepEqual(validateCapabilityArgs(schema, {}), []);
  assert.match(validateCapabilityArgs(schema, {forbidden: true})[0].message, /互斥字段|不允许/);
});

test("accepts subjectLists as a Kling Omni material source", () => {
  const schema = {
    type: "object",
    anyOf: [
      {required: ["referImages"]},
      {required: ["featureVideo"]},
      {required: ["subjectLists"]},
    ],
    properties: {
      referImages: {type: "array"},
      featureVideo: {type: "string"},
      subjectLists: {type: "array", minItems: 1, maxItems: 4, items: {type: "string"}},
    },
  };
  assert.deepEqual(validateCapabilityArgs(schema, {subjectLists: ["subject-a"]}, "agentk.kling_omni_video"), []);
  assert.equal(validateCapabilityArgs(schema, {subjectLists: []}, "agentk.kling_omni_video")[0].path, "subjectLists");
});

test("exposes capability cross-field constraints", () => {
  assert.deepEqual(validateCapabilityArgs({}, {startSeconds: 5, endSeconds: 5}, "agentk.understand_video_segment").map(error => error.path), ["endSeconds"]);
  assert.deepEqual(validateCapabilityArgs({}, {lastFrame: "https://example.com/end.png"}, "agentk.kling_omni_video").map(error => error.path), ["lastFrame"]);
  assert.deepEqual(validateCapabilityArgs({}, {referenceAudios: ["https://example.com/a.mp3"]}, "agentk.seedance_generate_video").map(error => error.path), ["referenceAudios"]);
  assert.deepEqual(validateCapabilityArgs({}, {lastFrame: "https://example.com/end.png"}, "agentk.kling_image_to_video").map(error => error.path), ["lastFrame"]);
  assert.deepEqual(validateCapabilityArgs({}, {featureVideo: "https://example.com/v.mp4", referImages: [1, 2, 3, 4, 5]}, "agentk.kling_omni_video").map(error => error.path), ["referImages"]);
  assert.deepEqual(validateCapabilityArgs({}, {soundStartTime: 3000, soundEndTime: 3000}, "agentk.lip_sync_video").map(error => error.path), ["soundEndTime"]);
  assert.deepEqual(validateCapabilityArgs({}, {soundStartTime: 1000, soundEndTime: 3000}, "agentk.lip_sync_video"), []);
});
