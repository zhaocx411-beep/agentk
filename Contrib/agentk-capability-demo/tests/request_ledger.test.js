const test = require("node:test");
const assert = require("node:assert/strict");

const {
  collectRequestEntries,
  eventTimings,
  findInputVideoUri,
  selectTaskInput,
  withObservedRequestTimes,
} = require("../src/agentk_capability_demo/static/request-ledger.js");

test("derives event start and end times from native timestamps and call pairs", () => {
  const requestAt = "2026-08-31T05:22:19.667482+00:00";
  const stageAt = "2026-08-31T05:22:20.000000+00:00";
  const responseAt = "2026-08-31T05:22:28.688165+00:00";
  const finishedAt = "2026-08-31T05:23:00.000000+00:00";
  const events = [
    { type: "progress", createdAt: requestAt, data: { kind: "request", callId: "call-1" } },
    { type: "progress", createdAt: stageAt, data: { percent: 20 } },
    { type: "progress", createdAt: responseAt, data: { kind: "response", callId: "call-1" } },
    { type: "finished", createdAt: finishedAt },
  ];

  assert.deepEqual(eventTimings(events), [
    { startedAt: requestAt, endedAt: responseAt },
    { startedAt: stageAt },
    { startedAt: requestAt, endedAt: responseAt },
    { endedAt: finishedAt },
  ]);
});

test("omits unavailable event times instead of using observed polling times", () => {
  const requestAt = "2026-08-31T06:00:00Z";
  const responseAt = "2026-08-31T06:00:10Z";
  const events = [
    { type: "progress", observedAt: requestAt, data: { kind: "request", callId: "pending" } },
    { type: "progress", createdAt: requestAt, data: { kind: "request", callId: "pending" } },
    { type: "progress", createdAt: responseAt, data: { kind: "response", callId: "orphan" } },
    { type: "progress", createdAt: "invalid", data: { percent: 50 } },
    null,
  ];

  assert.deepEqual(eventTimings(events), [
    {},
    { startedAt: requestAt },
    { endedAt: responseAt },
    {},
    {},
  ]);
  assert.deepEqual(eventTimings(null), []);
});

test("collects entry request and every recorded call in event order", () => {
  const entry = { pipeline: "documentary_video", args: { videoUri: "https://cdn.test/a.mp4" } };
  const events = [
    { id: "p1", type: "progress", step: "validate_input", data: { percent: 5 } },
    {
      id: "r1",
      type: "progress",
      step: "request",
      createdAt: "2026-08-27T04:15:31+00:00",
      data: {
        kind: "request",
        requestKind: "activity",
        name: "agentk.capability.probe_video",
        request: { videoUrl: "https://cdn.test/a.mp4" },
      },
    },
    {
      id: "r2",
      type: "progress",
      step: "request",
      observedAt: "2026-08-27T04:15:42+00:00",
      data: {
        kind: "request",
        requestKind: "workflow",
        name: "narrato.documentary.render_video.v2",
        request: { subtitles: true },
      },
    },
  ];

  assert.deepEqual(collectRequestEntries(entry, events, "2026-08-27T04:15:20+00:00"), [
    { id: "entry", kind: "ENTRY", name: "documentary_video", request: entry, requestedAt: "2026-08-27T04:15:20+00:00", requestedAtSource: "created" },
    {
      id: "r1",
      kind: "ACTIVITY",
      name: "agentk.capability.probe_video",
      request: { videoUrl: "https://cdn.test/a.mp4" },
      requestedAt: "2026-08-27T04:15:31+00:00",
      requestedAtSource: "created",
    },
    {
      id: "r2",
      kind: "WORKFLOW",
      name: "narrato.documentary.render_video.v2",
      request: { subtitles: true },
      requestedAt: "2026-08-27T04:15:42+00:00",
      requestedAtSource: "observed",
    },
  ]);
});

test("ignores ordinary progress events and malformed request payloads", () => {
  assert.deepEqual(
    collectRequestEntries(null, [
      { type: "progress", data: { percent: 20 } },
      { type: "progress", data: { kind: "request", request: "bad" } },
      { type: "finished", data: { kind: "request", request: {} } },
    ]),
    [],
  );
  assert.equal(
    collectRequestEntries({ pipeline: "draft", args: {} }, [], "invalid")[0].requestedAt,
    undefined,
  );
});

test("pairs concurrent repeated calls by call id when responses finish out of order", () => {
  const events = [
    { type: "progress", data: { kind: "request", requestKind: "activity", callId: "a", name: "tts", request: { text: "A" } } },
    { type: "progress", data: { kind: "request", requestKind: "activity", callId: "b", name: "tts", request: { text: "B" } } },
    { type: "progress", data: { kind: "response", responseKind: "activity", callId: "b", name: "tts", response: { uri: "b.mp3" } } },
    { type: "progress", data: { kind: "response", responseKind: "activity", callId: "a", name: "tts", response: { uri: "a.mp3" } } },
  ];
  const entries = collectRequestEntries(null, events);
  assert.deepEqual(
    entries.map(item => [item.request.text, item.response.uri]),
    [["A", "a.mp3"], ["B", "b.mp3"]],
  );
});

test("selects the stored entry request ahead of workflow event requests", () => {
  const entry = {
    pipeline: "documentary_video",
    args: { videoUri: "https://cdn.test/original.mp4", instructions: "keep this" },
  };
  const events = [
    {
      type: "progress",
      data: {
        kind: "request",
        requestKind: "workflow",
        name: "narrato.documentary.generate_script.v2",
        request: { sourceVideoUri: "https://cdn.test/workflow.mp4" },
      },
    },
  ];

  assert.equal(selectTaskInput(entry, events), entry);
});

test("normalizes the first workflow request and then the first recorded request", () => {
  const activity = { videoUrl: "https://cdn.test/probed.mp4" };
  const workflow = { sourceVideoUri: "https://cdn.test/workflow.mp4", theme: "science" };
  const events = [
    {
      type: "progress",
      data: {
        kind: "request",
        requestKind: "activity",
        name: "agentk.capability.probe_video",
        request: activity,
      },
    },
    {
      type: "progress",
      data: {
        kind: "request",
        requestKind: "workflow",
        name: "narrato.documentary.generate_script.v1",
        request: workflow,
      },
    },
  ];

  assert.deepEqual(selectTaskInput(null, events), {
    pipeline: "narrato.documentary.generate_script.v1",
    args: workflow,
  });
  assert.deepEqual(selectTaskInput(null, events.slice(0, 1)), {
    pipeline: "agentk.capability.probe_video",
    args: activity,
  });
  assert.equal(selectTaskInput(null, null), null);
});

test("does not double wrap a fallback request that is already a pipeline envelope", () => {
  const envelope = {
    pipeline: "documentary_video",
    args: { videoUri: "https://cdn.test/enveloped.mp4" },
  };
  const events = [
    {
      type: "progress",
      data: {
        kind: "request",
        requestKind: "workflow",
        name: "narrato.legacy.wrapper",
        request: envelope,
      },
    },
  ];

  assert.equal(selectTaskInput(null, events), envelope);
});

test("finds known video URI keys recursively", () => {
  assert.equal(
    findInputVideoUri({
      pipeline: "documentary_render",
      args: { script: { sourceVideoUri: " https://cdn.test/source.mp4?token=abc " } },
    }),
    "https://cdn.test/source.mp4?token=abc",
  );
  assert.equal(
    findInputVideoUri({ args: [{ options: {} }, { videoUrl: "http://media.test/input.webm" }] }),
    "http://media.test/input.webm",
  );
});

test("rejects unsafe video values and tolerates cyclic inputs", () => {
  const input = {
    videoUri: "javascript:alert(1)",
    nested: {
      videoUrl: "https://user:secret@cdn.test/private.mp4",
      source: { sourceVideoUri: "https://cdn.test/safe.mp4" },
    },
  };
  input.self = input;

  assert.equal(findInputVideoUri(input), "https://cdn.test/safe.mp4");
  assert.equal(findInputVideoUri({ videoUri: "//cdn.test/no-protocol.mp4" }), "");
  assert.equal(findInputVideoUri({ videoUri: 42 }), "");
  assert.equal(findInputVideoUri(null), "");
});

test("backfills stable first-seen times only for request events missing native times", () => {
  const remembered = new Map();
  const events = [
    { id: "request-1", type: "progress", data: { kind: "request", name: "probe" } },
    { id: "native", type: "progress", createdAt: "2026-08-27T06:00:00Z", data: { kind: "request", name: "tts" } },
    { id: "progress", type: "progress", data: { percent: 30 } },
  ];

  const first = withObservedRequestTimes(events, remembered, "2026-08-27T06:01:00Z");
  const second = withObservedRequestTimes(events, remembered, "2026-08-27T06:02:00Z");

  assert.equal(first[0].observedAt, "2026-08-27T06:01:00Z");
  assert.equal(second[0].observedAt, "2026-08-27T06:01:00Z");
  assert.equal(first[1], events[1]);
  assert.equal(first[2], events[2]);
  assert.equal(events[0].observedAt, undefined);
});

test("uses a valid observed time when a native event time is malformed", () => {
  const entries = collectRequestEntries(null, [
    {
      id: "legacy",
      type: "progress",
      createdAt: "not-a-time",
      observedAt: "2026-08-27T06:03:00Z",
      data: { kind: "request", requestKind: "activity", name: "probe", request: {} },
    },
  ]);
  assert.equal(entries[0].requestedAt, "2026-08-27T06:03:00Z");
  assert.equal(entries[0].requestedAtSource, "observed");

  const remembered = new Map();
  const backfilled = withObservedRequestTimes(
    [{ id: "bad", type: "progress", createdAt: "bad", data: { kind: "request" } }],
    remembered,
    "2026-08-27T06:04:00Z",
  );
  assert.equal(backfilled[0].observedAt, "2026-08-27T06:04:00Z");
});
