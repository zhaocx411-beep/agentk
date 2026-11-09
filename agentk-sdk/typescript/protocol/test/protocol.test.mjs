import assert from "node:assert/strict";
import test from "node:test";
import { create, fromBinary, fromJson, toBinary, toJson } from "@bufbuild/protobuf";
import { TimestampSchema, ValueSchema } from "@bufbuild/protobuf/wkt";
import { AgentKService, EventSchema, ExecutionSchema, SessionSchema } from "@ks-agentk/protocol/api/v1";
import { ArtifactSchema } from "@ks-agentk/protocol/common/v1";
import { InvocationKind } from "@ks-agentk/protocol/invocation/v1";

function roundTrip(schema, message) {
  assert.deepEqual(fromBinary(schema, toBinary(schema, message)), message);
  assert.deepEqual(fromJson(schema, toJson(schema, message)), message);
}

test("session and execution preserve timestamps and oneof values", () => {
  const createdAt = create(TimestampSchema, { seconds: 1700000000n, nanos: 123000000 });
  const session = create(SessionSchema, { id: "session-1", title: "Example", createdAt });
  roundTrip(SessionSchema, session);
  assert.equal(toJson(SessionSchema, session).createdAt, "2023-11-14T22:13:20.123Z");
  roundTrip(ExecutionSchema, create(ExecutionSchema, {
    id: "execution-1", sessionId: session.id,
    kind: { case: "pipeline", value: { name: "render", args: { count: 2, enabled: true } } },
  }));
});

test("event carries artifacts, Struct, Value, and duration through both formats", () => {
  const artifact = create(ArtifactSchema, {
    type: "image", uri: "https://example.com/image.png",
    params: { width: 640, tags: ["preview"], nested: { enabled: true } },
  });
  roundTrip(ArtifactSchema, artifact);
  const event = create(EventSchema, {
    eventId: "event-1",
    payload: { case: "toolExecSucceeded", value: {
      toolCallId: "call-1", artifacts: [artifact],
      result: fromJson(ValueSchema, { items: [1, true, null, "done"] }),
      duration: { seconds: 1n, nanos: 500000000 },
    } },
  });
  roundTrip(EventSchema, event);
  assert.equal(event.payload.case, "toolExecSucceeded");
  assert.deepEqual(toJson(EventSchema, event).toolExecSucceeded.result, { items: [1, true, null, "done"] });
  roundTrip(EventSchema, create(EventSchema));
});

test("events preserve invocation enum and Value data", () => {
  roundTrip(EventSchema, create(EventSchema, {
    payload: { case: "invocationProgress", value: {
      invocationKind: InvocationKind.TOOL, invocationId: "invocation-1",
      data: fromJson(ValueSchema, { progress: 0.5 }),
    } },
  }));
});

test("service exposes unary and server streaming method descriptors", () => {
  assert.equal(AgentKService.typeName, "agentk.api.v1.AgentKService");
  assert.equal(AgentKService.method.createSession.methodKind, "unary");
  assert.equal(AgentKService.method.createSession.input.typeName, "agentk.api.v1.CreateSessionRequest");
  assert.equal(AgentKService.method.watchEvents.methodKind, "server_streaming");
  assert.equal(AgentKService.method.watchEvents.output.typeName, "agentk.api.v1.WatchEventsResponse");
});

test("only documented package subpaths are importable", async () => {
  for (const specifier of ["@ks-agentk/protocol", "@ks-agentk/protocol/gen/agentk/api/v1/model_pb.js"]) {
    await assert.rejects(import(specifier), { code: "ERR_PACKAGE_PATH_NOT_EXPORTED" });
  }
});

test("aggregate, short, and long paths share schemas and descriptors", async () => {
  for (const [area, file] of [["api", "model"], ["api", "profile"], ["api", "event"], ["api", "service"], ["common", "common"], ["invocation", "invocation"]]) {
    const aggregate = await import(`@ks-agentk/protocol/${area}/v1`);
    const short = await import(`@ks-agentk/protocol/${area}/v1/${file}_pb.js`);
    const long = await import(`@ks-agentk/protocol/agentk/${area}/v1/${file}_pb.js`);
    for (const key of Object.keys(short)) {
      assert.equal(short[key], long[key]);
      assert.equal(short[key], aggregate[key]);
    }
  }
});
